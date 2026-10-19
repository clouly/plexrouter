// Package server wires the HTTP routes: the gateway API, the console JSON API
// and the embedded web console.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/local4a/plexrouter/internal/config"
	"github.com/local4a/plexrouter/internal/gateway"
	"github.com/local4a/plexrouter/internal/oauth"
	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/store"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	box     *secure.Box
	gw      *gateway.Gateway
	log     *slog.Logger
	version string
	web     fs.FS // built console (index.html + assets); may be empty
	limiter *rateLimiter
	oauth   map[string]*oauth.Provider // configured sign-in providers by name
}

func New(cfg *config.Config, st *store.Store, box *secure.Box, gw *gateway.Gateway, web fs.FS, version string, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, box: box, gw: gw, web: web, version: version, log: log,
		limiter: newRateLimiter(20, time.Minute), oauth: map[string]*oauth.Provider{}}
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		s.SetOAuthProvider(oauth.NewGoogle(cfg.GoogleClientID, cfg.GoogleClientSecret))
	}
	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		s.SetOAuthProvider(oauth.NewGitHub(cfg.GitHubClientID, cfg.GitHubClientSecret))
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.gw.Register(mux)

	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/models", s.handlePublicModels)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/oauth/{provider}", s.limited(s.handleOAuthStart))
	mux.HandleFunc("GET /api/auth/oauth/{provider}/callback", s.handleOAuthCallback)
	mux.HandleFunc("GET /api/me/identities", s.user(s.handleListIdentities))
	mux.HandleFunc("DELETE /api/me/identities/{provider}", s.user(s.handleUnlinkIdentity))
	mux.HandleFunc("GET /api/me", s.user(s.handleMe))

	mux.HandleFunc("GET /api/keys", s.user(s.handleListKeys))
	mux.HandleFunc("POST /api/keys", s.user(s.handleCreateKey))
	mux.HandleFunc("PATCH /api/keys/{id}", s.user(s.handleUpdateKey))
	mux.HandleFunc("DELETE /api/keys/{id}", s.user(s.handleDeleteKey))
	mux.HandleFunc("GET /api/usage/logs", s.user(s.handleUsageLogs))
	mux.HandleFunc("GET /api/usage/stats", s.user(s.handleUsageStats))
	mux.HandleFunc("GET /api/credits/transactions", s.user(s.handleTransactions))
	mux.HandleFunc("POST /api/credits/redeem", s.user(s.handleRedeem))

	mux.HandleFunc("GET /api/admin/stats", s.admin(s.handleAdminStats))
	mux.HandleFunc("GET /api/admin/providers", s.admin(s.handleListProviders))
	mux.HandleFunc("POST /api/admin/providers", s.admin(s.handleCreateProvider))
	mux.HandleFunc("PATCH /api/admin/providers/{id}", s.admin(s.handleUpdateProvider))
	mux.HandleFunc("DELETE /api/admin/providers/{id}", s.admin(s.handleDeleteProvider))
	mux.HandleFunc("POST /api/admin/providers/{id}/test", s.admin(s.handleTestProvider))
	mux.HandleFunc("GET /api/admin/providers/{id}/upstream-models", s.admin(s.handleUpstreamModels))
	mux.HandleFunc("GET /api/admin/models", s.admin(s.handleListModels))
	mux.HandleFunc("POST /api/admin/models", s.admin(s.handleCreateModel))
	mux.HandleFunc("POST /api/admin/models/import", s.admin(s.handleImportModels))
	mux.HandleFunc("PATCH /api/admin/models/{id}", s.admin(s.handleUpdateModel))
	mux.HandleFunc("DELETE /api/admin/models/{id}", s.admin(s.handleDeleteModel))
	mux.HandleFunc("GET /api/admin/users", s.admin(s.handleListUsers))
	mux.HandleFunc("PATCH /api/admin/users/{id}", s.admin(s.handleUpdateUser))
	mux.HandleFunc("POST /api/admin/users/{id}/credits", s.admin(s.handleAdjustCredits))
	mux.HandleFunc("GET /api/admin/logs", s.admin(s.handleAdminLogs))
	mux.HandleFunc("GET /api/admin/redeem-codes", s.admin(s.handleListCodes))
	mux.HandleFunc("POST /api/admin/redeem-codes", s.admin(s.handleCreateCodes))
	mux.HandleFunc("DELETE /api/admin/redeem-codes/{id}", s.admin(s.handleDeleteCode))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "unknown endpoint")
	})
	mux.Handle("/", s.spa())

	return s.recoverer(s.logRequests(s.sameOrigin(mux)))
}

// --- middleware ---

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "ip", s.clientIP(r))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "err", v, "stack", string(debug.Stack()))
				writeErr(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// sameOrigin blocks cross-site state-changing requests to the console API
// (CSRF). The gateway API is exempt: it authenticates with bearer keys.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && isConsoleAPI(r.URL.Path) {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
				if site != "same-origin" && site != "none" {
					writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request blocked")
					return
				}
			} else if origin := r.Header.Get("Origin"); origin != "" {
				if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
					writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request blocked")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isConsoleAPI(path string) bool {
	return strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/v1/")
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message, "code": code}})
}

// fail maps store errors to HTTP errors; anything unexpected is logged.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrEmailTaken):
		writeErr(w, http.StatusConflict, "email_taken", err.Error())
	case errors.Is(err, store.ErrSlugTaken):
		writeErr(w, http.StatusConflict, "slug_taken", err.Error())
	case errors.Is(err, store.ErrCodeInvalid):
		writeErr(w, http.StatusBadRequest, "invalid_code", err.Error())
	case errors.Is(err, context.Canceled):
	default:
		s.log.Error("request failed", "path", r.URL.Path, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

// handleHealth backs the Docker HEALTHCHECK: 200 only while the database is usable.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.log.Warn("health check failed", "err", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return 0, false
	}
	return id, true
}

func pageOf(r *http.Request) store.Page {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return store.Page{Page: p, Size: size}
}

func paged[T any](w http.ResponseWriter, data []T, total int) {
	if data == nil {
		data = []T{}
	}
	ok(w, map[string]any{"data": data, "total": total})
}

// Optional distinguishes an absent JSON field from an explicit null.
type Optional[T any] struct {
	Set   bool
	Value T
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
}

func (s *Server) clientIP(r *http.Request) string {
	if !s.cfg.TrustProxy {
		return remoteHost(r)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(ip)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return remoteHost(r)
}

func remoteHost(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

// --- web console ---

const notBuiltPage = `<!doctype html><meta charset="utf-8"><title>PlexRouter</title>
<body style="font-family:system-ui;max-width:40rem;margin:4rem auto;padding:0 1rem;line-height:1.5">
<h1>PlexRouter is running</h1>
<p>The API is available at <code>/v1</code>, but the web console was not built into this binary.</p>
<p>Build it with <code>make build</code> (or <code>cd web &amp;&amp; npm ci &amp;&amp; npm run build</code>, then rebuild the server).</p>`

// spa serves the built console, falling back to index.html for client routes.
func (s *Server) spa() http.Handler {
	_, err := fs.Stat(s.web, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(notBuiltPage))
		})
	}
	files := http.FileServerFS(s.web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if st, err := fs.Stat(s.web, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.web, "index.html")
	})
}
