package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/store"
)

const (
	sessionCookie = "plex_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type ctxKey struct{}

func currentUser(r *http.Request) *store.User { return r.Context().Value(ctxKey{}).(*store.User) }

func sessionHash(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	return secure.HashToken(c.Value)
}

var errNoSession = errors.New("not logged in")

// sessionUser returns the enabled user behind the request's session cookie.
func (s *Server) sessionUser(r *http.Request) (*store.User, error) {
	hash := sessionHash(r)
	if hash == "" {
		return nil, errNoSession
	}
	u, err := s.store.SessionUser(r.Context(), hash)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.Disabled) {
		return nil, errNoSession
	}
	return u, err
}

// user requires a logged-in, enabled account.
func (s *Server) user(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.sessionUser(r)
		if errors.Is(err, errNoSession) {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "not logged in")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return s.user(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			writeErr(w, http.StatusForbidden, "forbidden", "admin only")
			return
		}
		next(w, r)
	})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	token := secure.RandomHex(32)
	if err := s.store.CreateSession(r.Context(), secure.HashToken(token), u.ID, sessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	return nil
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"name": s.cfg.Name, "version": s.version, "allow_register": s.cfg.AllowRegister,
		"oauth_providers": s.oauthProviderNames()})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if hash := sessionHash(r); hash != "" {
		s.store.DeleteSession(r.Context(), hash)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
	ok(w, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"user": userDTO(currentUser(r))})
}

// rateLimiter is a fixed-window limiter keyed by client IP, used for
// sign-in and redeeming codes.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, per time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: per, hits: map[string]*window{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 10000 { // bound memory
		for k, w := range l.hits {
			if now.Sub(w.start) > l.window {
				delete(l.hits, k)
			}
		}
	}
	w := l.hits[key]
	if w == nil || now.Sub(w.start) > l.window {
		l.hits[key] = &window{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.limit
}

func (s *Server) limited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(s.clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again in a minute")
			return
		}
		next(w, r)
	}
}
