package server

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/clouly/plexrouter/internal/secure"
	"github.com/clouly/plexrouter/internal/store"
)

const (
	sessionCookie = "plex_session"
	sessionTTL    = 30 * 24 * time.Hour
	minPassword   = 8
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

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// The first account (the admin) can always be created.
	if !s.cfg.AllowRegister && n > 0 {
		writeErr(w, http.StatusForbidden, "registration_closed", "registration is disabled")
		return
	}
	in.Email = strings.TrimSpace(in.Email)
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		writeErr(w, http.StatusBadRequest, "invalid_email", "invalid email address")
		return
	}
	if len(in.Password) < minPassword {
		writeErr(w, http.StatusBadRequest, "weak_password", "password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name, _, _ = strings.Cut(in.Email, "@")
	}
	u, err := s.store.CreateUser(r.Context(), in.Email, name, string(hash), store.FromUSD(s.cfg.SignupCredit))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"user": userDTO(u)})
}

// dummyHash keeps login timing similar for unknown emails.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("plexrouter-dummy-password"), bcrypt.DefaultCost)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	u, err := s.store.GetUserByEmail(r.Context(), in.Email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, err)
		return
	}
	if u == nil || !u.HasPassword() {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
		writeErr(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	if u.Disabled {
		writeErr(w, http.StatusForbidden, "account_disabled", "this account is disabled")
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"user": userDTO(u)})
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

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := currentUser(r)
	// OAuth-only accounts set their first password without an old one.
	if u.HasPassword() && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Old)) != nil {
		writeErr(w, http.StatusBadRequest, "wrong_password", "current password is incorrect")
		return
	}
	if len(in.New) < minPassword {
		writeErr(w, http.StatusBadRequest, "weak_password", "password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.New), bcrypt.DefaultCost)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), u.ID, string(hash)); err != nil {
		s.fail(w, r, err)
		return
	}
	s.store.DeleteOtherSessions(r.Context(), u.ID, sessionHash(r))
	ok(w, map[string]any{"ok": true})
}

// rateLimiter is a fixed-window limiter keyed by client IP, used for login
// and registration.
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
