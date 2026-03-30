package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
	"golang.org/x/crypto/bcrypt"
)

func TestMiddlewareAcceptsBcryptSecret(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("plain-secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword failed: %v", err)
	}

	manager, err := ops.NewManager(ops.Options{
		BaseDir:       baseDir,
		WorkspaceRoot: baseDir,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	cfg := ops.DeployConfig{
		BaseDir:                baseDir,
		DataDir:                filepath.Join(baseDir, "data"),
		ConfigFile:             filepath.Join(baseDir, "data", "config.yaml"),
		AllowRemoteManagement:  true,
		ManagementSecret:       string(hash),
		ManagementSecretHashed: true,
		RequestRetry:           1,
	}
	if err = os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err = os.WriteFile(cfg.ConfigFile, []byte("port: 8317\nremote-management:\n  allow-remote: true\n  secret-key: "+string(hash)+"\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	authenticator := New(manager)
	handler := authenticator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/ops/api/status", nil)
	request.Header.Set("Authorization", "Bearer plain-secret")
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
