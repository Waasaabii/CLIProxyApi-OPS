package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const tokenContextKey contextKey = "management-token"

type attemptInfo struct {
	count        int
	blockedUntil time.Time
	lastActivity time.Time
}

type Authenticator struct {
	manager *ops.Manager

	mu       sync.Mutex
	attempts map[string]*attemptInfo
}

func New(manager *ops.Manager) *Authenticator {
	auth := &Authenticator{
		manager:  manager,
		attempts: map[string]*attemptInfo{},
	}
	go auth.cleanupLoop()
	return auth
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := a.manager.CurrentConfig()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "config_load_failed", err)
			return
		}

		clientIP := clientIPFromRequest(r)
		localClient := isLocalClient(clientIP)
		if !localClient {
			if blockErr := a.checkBlocked(clientIP); blockErr != nil {
				writeJSONError(w, http.StatusForbidden, "ip_blocked", blockErr)
				return
			}
			if !cfg.AllowRemoteManagement {
				writeJSONError(w, http.StatusForbidden, "remote_management_disabled", errors.New("remote management disabled"))
				return
			}
		}

		secret := strings.TrimSpace(cfg.ManagementSecret)
		if secret == "" {
			writeJSONError(w, http.StatusForbidden, "secret_not_set", errors.New("remote management key not set"))
			return
		}

		provided := extractToken(r)
		if provided == "" {
			if !localClient {
				a.recordFailure(clientIP)
			}
			writeJSONError(w, http.StatusUnauthorized, "missing_management_key", errors.New("missing management key"))
			return
		}

		if !matchSecret(secret, provided) {
			if !localClient {
				a.recordFailure(clientIP)
			}
			writeJSONError(w, http.StatusUnauthorized, "invalid_management_key", errors.New("invalid management key"))
			return
		}

		if !localClient {
			a.clearFailure(clientIP)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenContextKey, provided)))
	})
}

func TokenFromContext(ctx context.Context) string {
	value, _ := ctx.Value(tokenContextKey).(string)
	return value
}

func (a *Authenticator) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		a.mu.Lock()
		for ip, attempt := range a.attempts {
			if !attempt.blockedUntil.IsZero() && now.Before(attempt.blockedUntil) {
				continue
			}
			if now.Sub(attempt.lastActivity) > 2*time.Hour {
				delete(a.attempts, ip)
			}
		}
		a.mu.Unlock()
	}
}

func (a *Authenticator) checkBlocked(ip string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	attempt := a.attempts[ip]
	if attempt == nil {
		return nil
	}
	if attempt.blockedUntil.IsZero() || time.Now().After(attempt.blockedUntil) {
		return nil
	}
	return fmt.Errorf("IP 已临时封禁，%s 后再试", time.Until(attempt.blockedUntil).Round(time.Second))
}

func (a *Authenticator) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	attempt := a.attempts[ip]
	if attempt == nil {
		attempt = &attemptInfo{}
		a.attempts[ip] = attempt
	}
	attempt.count++
	attempt.lastActivity = time.Now()
	if attempt.count >= 5 {
		attempt.count = 0
		attempt.blockedUntil = time.Now().Add(30 * time.Minute)
	}
}

func (a *Authenticator) clearFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	attempt := a.attempts[ip]
	if attempt == nil {
		return
	}
	attempt.count = 0
	attempt.blockedUntil = time.Time{}
	attempt.lastActivity = time.Now()
}

func extractToken(r *http.Request) string {
	if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		return authHeader
	}
	return strings.TrimSpace(r.Header.Get("X-Management-Key"))
}

func matchSecret(secret, provided string) bool {
	if secret == "" || provided == "" {
		return false
	}
	if strings.HasPrefix(secret, "$2a$") || strings.HasPrefix(secret, "$2b$") || strings.HasPrefix(secret, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(secret), []byte(provided)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) == 1
}

func clientIPFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func isLocalClient(ip string) bool {
	if ip == "" {
		return false
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return parsedIP.IsLoopback()
}

func writeJSONError(w http.ResponseWriter, status int, code string, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":"%s","message":%q}`, code, err.Error())
}
