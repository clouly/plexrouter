package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/clouly/plexrouter/internal/oauth"
	"github.com/clouly/plexrouter/internal/secure"
	"github.com/clouly/plexrouter/internal/store"
)

const (
	oauthCookie = "plex_oauth"
	oauthTTL    = 10 * time.Minute
)

// SetOAuthProvider registers (or replaces) a sign-in provider.
func (s *Server) SetOAuthProvider(p *oauth.Provider) { s.oauth[p.Name] = p }

func (s *Server) oauthProviderNames() []string {
	names := []string{}
	for _, n := range []string{oauth.Google, oauth.GitHub} {
		if _, ok := s.oauth[n]; ok {
			names = append(names, n)
		}
	}
	return names
}

// oauthAttempt is kept in a short-lived HttpOnly cookie between the redirect
// to the provider and the callback.
type oauthAttempt struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Provider string `json:"p"`
	Link     bool   `json:"l"`
}

func (s *Server) publicBase(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" && s.cfg.TrustProxy {
		host = h
	}
	return scheme + "://" + host
}

func (s *Server) callbackURL(r *http.Request, provider string) string {
	return s.publicBase(r) + "/api/auth/oauth/" + provider + "/callback"
}

func oauthFail(w http.ResponseWriter, r *http.Request, link bool, code string) {
	page := "/login"
	if link {
		page = "/settings"
	}
	http.Redirect(w, r, page+"?oauth_error="+url.QueryEscape(code), http.StatusFound)
}

// handleOAuthStart redirects to the provider. ?mode=link attaches the
// provider to the logged-in account instead of signing in.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	link := r.URL.Query().Get("mode") == "link"
	p, ok := s.oauth[r.PathValue("provider")]
	if !ok {
		oauthFail(w, r, link, "not_configured")
		return
	}
	if link {
		if _, err := s.sessionUser(r); err != nil {
			oauthFail(w, r, false, "not_logged_in")
			return
		}
	}
	a := oauthAttempt{State: secure.RandomHex(16), Verifier: oauth.NewVerifier(), Provider: p.Name, Link: link}
	b, _ := json.Marshal(a)
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: base64.RawURLEncoding.EncodeToString(b), Path: "/api/auth/oauth/",
		MaxAge: int(oauthTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	http.Redirect(w, r, p.AuthCodeURL(s.callbackURL(r, p.Name), a.State, a.Verifier), http.StatusFound)
}

func readAttempt(r *http.Request) (*oauthAttempt, bool) {
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		return nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, false
	}
	var a oauthAttempt
	if json.Unmarshal(b, &a) != nil || a.State == "" {
		return nil, false
	}
	return &a, true
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	a, ok := readAttempt(r)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/api/auth/oauth/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
	link := ok && a.Link
	q := r.URL.Query()
	if !ok || a.Provider != name || q.Get("state") != a.State {
		oauthFail(w, r, link, "state_mismatch")
		return
	}
	p, ok := s.oauth[name]
	if !ok {
		oauthFail(w, r, link, "not_configured")
		return
	}
	if e := q.Get("error"); e != "" {
		code := "provider_error"
		if e == "access_denied" {
			code = "access_denied"
		}
		oauthFail(w, r, link, code)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	prof, err := p.Exchange(ctx, s.callbackURL(r, name), q.Get("code"), a.Verifier)
	if err != nil {
		s.log.Warn("oauth exchange failed", "provider", name, "err", err)
		oauthFail(w, r, link, "provider_error")
		return
	}
	id := store.Identity{Provider: name, Subject: prof.Subject, Email: prof.Email}

	if link {
		s.oauthLink(w, r, id)
		return
	}
	u, code, err := s.oauthSignIn(r.Context(), prof, id)
	if err != nil {
		s.log.Error("oauth sign-in", "provider", name, "err", err)
		oauthFail(w, r, false, "provider_error")
		return
	}
	if code != "" {
		oauthFail(w, r, false, code)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		s.log.Error("oauth session", "err", err)
		oauthFail(w, r, false, "provider_error")
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// oauthSignIn finds or creates the account for a provider profile. It returns
// a non-empty error code when sign-in is refused.
func (s *Server) oauthSignIn(ctx context.Context, prof *oauth.Profile, id store.Identity) (*store.User, string, error) {
	u, err := s.store.IdentityUser(ctx, id.Provider, id.Subject)
	if err == nil {
		if u.Disabled {
			return nil, "account_disabled", nil
		}
		return u, "", nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, "", err
	}
	// Without a verified email we can neither link to an existing account
	// nor create one safely.
	if prof.Email == "" || !prof.EmailVerified {
		return nil, "email_unverified", nil
	}

	u, err = s.store.GetUserByEmail(ctx, prof.Email)
	switch {
	case err == nil:
		if u.Disabled {
			return nil, "account_disabled", nil
		}
		id.UserID = u.ID
		if err := s.store.LinkIdentity(ctx, id); err != nil {
			if errors.Is(err, store.ErrIdentityTaken) {
				return nil, "already_linked", nil
			}
			return nil, "", err
		}
		return u, "", nil
	case !errors.Is(err, store.ErrNotFound):
		return nil, "", err
	}

	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, "", err
	}
	if !s.cfg.AllowRegister && n > 0 {
		return nil, "registration_closed", nil
	}
	name := prof.Name
	if name == "" {
		name = prof.Email
	}
	u, err = s.store.CreateOAuthUser(ctx, prof.Email, name, store.FromUSD(s.cfg.SignupCredit), id)
	if errors.Is(err, store.ErrIdentityTaken) || errors.Is(err, store.ErrEmailTaken) {
		return nil, "already_linked", nil // lost a race with a concurrent sign-in
	}
	return u, "", err
}

func (s *Server) oauthLink(w http.ResponseWriter, r *http.Request, id store.Identity) {
	u, err := s.sessionUser(r)
	if err != nil {
		oauthFail(w, r, false, "not_logged_in")
		return
	}
	if owner, err := s.store.IdentityUser(r.Context(), id.Provider, id.Subject); err == nil && owner.ID != u.ID {
		oauthFail(w, r, true, "already_linked")
		return
	}
	id.UserID = u.ID
	if err := s.store.LinkIdentity(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrIdentityTaken) {
			oauthFail(w, r, true, "already_linked")
			return
		}
		s.log.Error("oauth link", "err", err)
		oauthFail(w, r, true, "provider_error")
		return
	}
	http.Redirect(w, r, "/settings?linked="+url.QueryEscape(id.Provider), http.StatusFound)
}

func (s *Server) handleListIdentities(w http.ResponseWriter, r *http.Request) {
	ids, err := s.store.ListIdentities(r.Context(), currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type identityJSON struct {
		Provider  string    `json:"provider"`
		Email     string    `json:"email"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := make([]identityJSON, 0, len(ids))
	for _, i := range ids {
		out = append(out, identityJSON{i.Provider, i.Email, i.CreatedAt})
	}
	ok(w, map[string]any{"data": out})
}

func (s *Server) handleUnlinkIdentity(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	provider := r.PathValue("provider")
	ids, err := s.store.ListIdentities(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	others := 0
	for _, i := range ids {
		if i.Provider != provider {
			others++
		}
	}
	if !u.HasPassword() && others == 0 {
		writeErr(w, http.StatusBadRequest, "last_login_method", "set a password or link another account before disconnecting this one")
		return
	}
	if err := s.store.UnlinkIdentity(r.Context(), u.ID, provider); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"ok": true})
}
