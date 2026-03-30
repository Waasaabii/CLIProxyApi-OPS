package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/auth"
	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
	"github.com/Waasaabii/CLIProxyApi-OPS/internal/tasks"
)

//go:embed management.js
var assets embed.FS

type Server struct {
	manager         *ops.Manager
	listen          string
	auth            *auth.Authenticator
	tasks           *tasks.Manager
	httpSrv         *http.Server
	versionPayloads versionPayloadFactory
}

func New(manager *ops.Manager, listenAddr string) (*Server, error) {
	cfg, err := manager.CurrentConfig()
	if err != nil {
		return nil, err
	}
	taskManager, err := tasks.New(cfg.BaseDir)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		manager:         manager,
		listen:          listenAddr,
		auth:            auth.New(manager),
		tasks:           taskManager,
		versionPayloads: defaultVersionPayloadFactory{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ops/api/health", srv.handleHealth)
	mux.Handle("/ops/api/", srv.auth.Middleware(http.HandlerFunc(srv.handleAPI)))
	mux.Handle("/v0/management/latest-version", srv.auth.Middleware(http.HandlerFunc(srv.handleLegacyLatestVersion)))
	mux.HandleFunc("/ops/management.js", srv.handleManagementJS)
	mux.HandleFunc("/", srv.handleProxy)
	srv.httpSrv = &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv, nil
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleManagementJS(w http.ResponseWriter, r *http.Request) {
	data, err := assets.ReadFile("management.js")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleLegacyLatestVersion(w http.ResponseWriter, r *http.Request) {
	version, err := s.manager.CheckUpdate(r.Context(), auth.TokenFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, s.versionPayloads.Legacy().Build(version))
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/ops/api/info":
		info, err := s.manager.Info(r.Context(), auth.TokenFromContext(r.Context()))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	case r.Method == http.MethodGet && r.URL.Path == "/ops/api/status":
		status, err := s.manager.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case r.Method == http.MethodGet && r.URL.Path == "/ops/api/version":
		version, err := s.manager.CheckUpdate(r.Context(), auth.TokenFromContext(r.Context()))
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, s.versionPayloads.Standard().Build(version))
	case r.Method == http.MethodGet && r.URL.Path == "/ops/api/release-notes":
		release, err := s.manager.LatestReleaseNotes(r.Context(), r.URL.Query().Get("locale"), auth.TokenFromContext(r.Context()))
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, release)
	case r.Method == http.MethodGet && r.URL.Path == "/ops/api/logs":
		content, err := s.manager.ReadOperationLog(r.Context(), parseIntDefault(r.URL.Query().Get("lines"), 200))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": content})
	case r.Method == http.MethodPost && r.URL.Path == "/ops/api/backup":
		snapshot, err := s.manager.Backup(r.Context(), s.manager.ConsoleLogger())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case r.Method == http.MethodPost && r.URL.Path == "/ops/api/install":
		s.startTask(w, r, "install", func(ctx context.Context, logger ops.Logger) error {
			return s.manager.Install(ctx, logger)
		})
	case r.Method == http.MethodPost && r.URL.Path == "/ops/api/update":
		token := auth.TokenFromContext(r.Context())
		s.startTask(w, r, "update", func(ctx context.Context, logger ops.Logger) error {
			return s.manager.Update(ctx, logger, token)
		})
	case r.Method == http.MethodPost && r.URL.Path == "/ops/api/repair":
		s.startTask(w, r, "repair", func(ctx context.Context, logger ops.Logger) error {
			return s.manager.Repair(ctx, logger)
		})
	case r.Method == http.MethodPost && r.URL.Path == "/ops/api/restore":
		var payload struct {
			Snapshot string `json:"snapshot"`
		}
		if err := decodeJSONBody(r, &payload); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.startTask(w, r, "restore", func(ctx context.Context, logger ops.Logger) error {
			return s.manager.Restore(ctx, logger, payload.Snapshot)
		})
	case r.Method == http.MethodPost && r.URL.Path == "/ops/api/uninstall":
		var payload struct {
			DryRun       bool `json:"dryRun"`
			PurgeData    bool `json:"purgeData"`
			PurgeBackups bool `json:"purgeBackups"`
		}
		if err := decodeJSONBody(r, &payload); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if payload.DryRun {
			result, err := s.manager.Uninstall(r.Context(), s.manager.ConsoleLogger(), ops.UninstallOptions{
				DryRun:       true,
				PurgeData:    payload.PurgeData,
				PurgeBackups: payload.PurgeBackups,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		s.startTask(w, r, "uninstall", func(ctx context.Context, logger ops.Logger) error {
			_, err := s.manager.Uninstall(ctx, logger, ops.UninstallOptions{
				DryRun:       false,
				PurgeData:    payload.PurgeData,
				PurgeBackups: payload.PurgeBackups,
			})
			return err
		})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ops/api/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/ops/api/tasks/")
		task, err := s.tasks.Get(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		logContent, err := s.tasks.ReadLog(id, 32*1024)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         task.ID,
			"name":       task.Name,
			"status":     task.Status,
			"error":      task.Error,
			"logPath":    task.LogPath,
			"createdAt":  task.CreatedAt,
			"startedAt":  task.StartedAt,
			"finishedAt": task.FinishedAt,
			"updatedAt":  task.UpdatedAt,
			"log":        logContent,
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
	}
}

func (s *Server) startTask(w http.ResponseWriter, r *http.Request, name string, fn func(context.Context, ops.Logger) error) {
	task, err := s.tasks.StartMutatingTask(r.Context(), name, fn)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	target, err := s.currentUpstreamURL()
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = target.Host
		req.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if !shouldInject(resp) {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		injected, changed := injectManagementScript(body)
		if !changed {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
			return nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(injected))
		resp.ContentLength = int64(len(injected))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(injected)))
		return nil
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, proxyErr error) {
		writeError(writer, http.StatusBadGateway, proxyErr)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) currentUpstreamURL() (*url.URL, error) {
	raw, err := s.manager.UpstreamBaseURL()
	if err != nil {
		return nil, err
	}
	return url.Parse(raw)
}

func shouldInject(resp *http.Response) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.Contains(contentType, "text/html")
}

func injectManagementScript(body []byte) ([]byte, bool) {
	if bytes.Contains(body, []byte(`/ops/management.js`)) {
		return body, false
	}
	index := lastIndexFoldASCII(body, []byte("</body>"))
	if index < 0 {
		return body, false
	}
	snippet := []byte(`<script src="/ops/management.js"></script>`)
	result := append([]byte{}, body[:index]...)
	result = append(result, snippet...)
	result = append(result, body[index:]...)
	return result, true
}

func decodeJSONBody(r *http.Request, target any) error {
	if r.Body == nil {
		return io.EOF
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error":   http.StatusText(status),
		"message": err.Error(),
	})
}

func parseIntDefault(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func lastIndexFoldASCII(body, needle []byte) int {
	if len(needle) == 0 || len(body) < len(needle) {
		return -1
	}
	for i := len(body) - len(needle); i >= 0; i-- {
		match := true
		for j := 0; j < len(needle); j++ {
			if asciiLower(body[i+j]) != asciiLower(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func asciiLower(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + 32
	}
	return value
}
