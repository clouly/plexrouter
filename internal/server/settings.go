package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/local4a/plexrouter/internal/gateway"
	"github.com/local4a/plexrouter/internal/oauth"
)

// System settings are edited in the console (Admin → System settings) and
// stored in the database. A setting that was never saved there falls back
// to its environment variable (already parsed into config.Config), then to
// the built-in default.

const (
	keySiteName       = "site_name"
	keyPublicURL      = "public_url"
	keyAllowRegister  = "allow_register"
	keySignupCredit   = "signup_credit"
	keyDefaultUserRPM = "default_user_rpm"
	keyRequestTimeout = "request_timeout_seconds"
	keyLogRetention   = "log_retention_days"
	keyTrustProxy     = "trust_proxy"
	keyGitHubID       = "github_client_id"
	keyGitHubSecret   = "github_client_secret" // encrypted
	keyGoogleID       = "google_client_id"
	keyGoogleSecret   = "google_client_secret" // encrypted
)

var settingEnv = map[string]string{
	keySiteName: "PLEXROUTER_NAME", keyPublicURL: "PLEXROUTER_PUBLIC_URL", keyAllowRegister: "PLEXROUTER_ALLOW_REGISTER",
	keySignupCredit: "PLEXROUTER_SIGNUP_CREDIT", keyDefaultUserRPM: "PLEXROUTER_DEFAULT_USER_RPM",
	keyRequestTimeout: "PLEXROUTER_REQUEST_TIMEOUT", keyLogRetention: "PLEXROUTER_LOG_RETENTION_DAYS",
	keyTrustProxy: "PLEXROUTER_TRUST_PROXY", keyGitHubID: "PLEXROUTER_GITHUB_CLIENT_ID",
	keyGoogleID: "PLEXROUTER_GOOGLE_CLIENT_ID",
}

// reloadSettings reads saved settings and applies those the gateway uses.
func (s *Server) reloadSettings(ctx context.Context) error {
	vals, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	s.settingsMu.Lock()
	s.settings = vals
	s.settingsMu.Unlock()
	s.gw.SetOptions(gateway.Options{Timeout: s.requestTimeout(), DefaultUserRPM: s.defaultUserRPM()})
	return nil
}

func (s *Server) saved(key string) (string, bool) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	v, ok := s.settings[key]
	return v, ok
}

func (s *Server) source(key string) string {
	if _, ok := s.saved(key); ok {
		return "console"
	}
	if env := settingEnv[key]; env != "" && os.Getenv(env) != "" {
		return "env"
	}
	return "default"
}

func (s *Server) siteName() string {
	if v, ok := s.saved(keySiteName); ok && v != "" {
		return v
	}
	return s.cfg.Name
}

func (s *Server) publicURL() string {
	if v, ok := s.saved(keyPublicURL); ok {
		return v
	}
	return s.cfg.PublicURL
}

func (s *Server) allowRegister() bool {
	if v, ok := s.saved(keyAllowRegister); ok {
		return v == "true"
	}
	return s.cfg.AllowRegister
}

func (s *Server) signupCredit() float64 {
	if v, ok := s.saved(keySignupCredit); ok {
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return s.cfg.SignupCredit
}

func (s *Server) defaultUserRPM() int {
	if v, ok := s.saved(keyDefaultUserRPM); ok {
		n, _ := strconv.Atoi(v)
		return n
	}
	return s.cfg.DefaultUserRPM
}

func (s *Server) requestTimeout() time.Duration {
	if v, ok := s.saved(keyRequestTimeout); ok {
		if n, _ := strconv.Atoi(v); n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	if s.cfg.RequestTimeout > 0 {
		return s.cfg.RequestTimeout
	}
	return 10 * time.Minute
}

// LogRetentionDays is how long request logs are kept; 0 means forever.
func (s *Server) LogRetentionDays() int {
	if v, ok := s.saved(keyLogRetention); ok {
		n, _ := strconv.Atoi(v)
		return n
	}
	return s.cfg.LogRetention
}

func (s *Server) trustProxy() bool {
	if v, ok := s.saved(keyTrustProxy); ok {
		return v == "true"
	}
	return s.cfg.TrustProxy
}

// oauthCreds returns a provider's client ID and secret ("" if unset).
func (s *Server) oauthCreds(provider string) (id, secret string) {
	idKey, secretKey, envID, envSecret := keyGitHubID, keyGitHubSecret, s.cfg.GitHubClientID, s.cfg.GitHubClientSecret
	if provider == oauth.Google {
		idKey, secretKey, envID, envSecret = keyGoogleID, keyGoogleSecret, s.cfg.GoogleClientID, s.cfg.GoogleClientSecret
	}
	id, secret = envID, envSecret
	if v, ok := s.saved(idKey); ok {
		id = v
		if enc, ok := s.saved(secretKey); ok {
			secret, _ = s.box.Decrypt(enc)
		}
	}
	return id, secret
}

// oauthProvider returns a configured sign-in provider.
func (s *Server) oauthProvider(name string) (*oauth.Provider, bool) {
	if p, ok := s.oauthOverride[name]; ok {
		return p, true
	}
	id, secret := s.oauthCreds(name)
	if id == "" || secret == "" {
		return nil, false
	}
	switch name {
	case oauth.GitHub:
		return oauth.NewGitHub(id, secret), true
	case oauth.Google:
		return oauth.NewGoogle(id, secret), true
	}
	return nil, false
}

func (s *Server) setupRequired(ctx context.Context) (bool, error) {
	has, err := s.store.HasAdmin(ctx)
	return !has, err
}

func validPublicURL(u string) bool {
	if u == "" {
		return true
	}
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != "" && (p.Path == "" || p.Path == "/")
}

func strPtr(s string) *string { return &s }

// --- first-run setup ---

func (s *Server) callbackURLs(base string) map[string]string {
	return map[string]string{
		oauth.GitHub: base + "/api/auth/oauth/github/callback",
		oauth.Google: base + "/api/auth/oauth/google/callback",
	}
}

func (s *Server) handleSetupInfo(w http.ResponseWriter, r *http.Request) {
	required, err := s.setupRequired(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	base := s.publicBase(r)
	ok(w, map[string]any{"setup_required": required, "suggested_public_url": base, "callback_urls": s.callbackURLs(base)})
}

type oauthAppInput struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// handleSetup saves the first sign-in configuration. Anyone may call it until
// an admin exists; the first person to sign in afterwards becomes the admin.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	required, err := s.setupRequired(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !required {
		writeErr(w, http.StatusForbidden, "setup_done", "this instance is already set up")
		return
	}
	var in struct {
		SiteName  string         `json:"site_name"`
		PublicURL string         `json:"public_url"`
		GitHub    *oauthAppInput `json:"github"`
		Google    *oauthAppInput `json:"google"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.PublicURL = strings.TrimRight(strings.TrimSpace(in.PublicURL), "/")
	if !validPublicURL(in.PublicURL) {
		writeErr(w, http.StatusBadRequest, "invalid_request", "public_url must look like https://api.example.com")
		return
	}
	values := map[string]*string{}
	if name := strings.TrimSpace(in.SiteName); name != "" {
		if len(name) > 100 {
			writeErr(w, http.StatusBadRequest, "invalid_request", "site_name is too long")
			return
		}
		values[keySiteName] = &name
	}
	if in.PublicURL != "" {
		values[keyPublicURL] = &in.PublicURL
	}
	providers := []string{}
	for _, p := range []struct {
		name, idKey, secretKey string
		app                    *oauthAppInput
	}{{oauth.GitHub, keyGitHubID, keyGitHubSecret, in.GitHub}, {oauth.Google, keyGoogleID, keyGoogleSecret, in.Google}} {
		if p.app == nil {
			continue
		}
		id, secret := strings.TrimSpace(p.app.ClientID), strings.TrimSpace(p.app.ClientSecret)
		if id == "" || secret == "" {
			continue
		}
		values[p.idKey], values[p.secretKey] = strPtr(id), strPtr(s.box.Encrypt(secret))
		providers = append(providers, p.name)
	}
	if len(providers) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "enter the client ID and secret of a GitHub or Google OAuth app")
		return
	}
	if err := s.store.SaveSettings(r.Context(), values); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.reloadSettings(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("first-run setup saved; the next sign-in becomes the admin", "providers", providers)
	ok(w, map[string]any{"ok": true, "oauth_providers": providers})
}

// --- admin settings ---

func (s *Server) settingsView(r *http.Request) map[string]any {
	ghID, ghSecret := s.oauthCreds(oauth.GitHub)
	goID, goSecret := s.oauthCreds(oauth.Google)
	sources := map[string]string{}
	for _, k := range []string{keySiteName, keyPublicURL, keyAllowRegister, keySignupCredit, keyDefaultUserRPM, keyRequestTimeout,
		keyLogRetention, keyTrustProxy} {
		sources[k] = s.source(k)
	}
	sources["github"], sources["google"] = s.source(keyGitHubID), s.source(keyGoogleID)
	return map[string]any{
		"settings": map[string]any{
			"site_name": s.siteName(), "public_url": s.publicURL(), "allow_register": s.allowRegister(),
			"signup_credit": s.signupCredit(), "default_user_rpm": s.defaultUserRPM(),
			"request_timeout_seconds": int(s.requestTimeout().Seconds()), "log_retention_days": s.LogRetentionDays(),
			"trust_proxy": s.trustProxy(),
			"github":      map[string]any{"client_id": ghID, "client_secret_set": ghSecret != ""},
			"google":      map[string]any{"client_id": goID, "client_secret_set": goSecret != ""},
		},
		"sources":       sources,
		"callback_urls": s.callbackURLs(s.publicBase(r)),
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ok(w, s.settingsView(r))
}

type oauthAppPatch struct {
	ClientID     Optional[*string] `json:"client_id"`
	ClientSecret *string           `json:"client_secret"`
}

var errBadSetting = errors.New("bad setting")

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SiteName       Optional[*string]  `json:"site_name"`
		PublicURL      Optional[*string]  `json:"public_url"`
		AllowRegister  Optional[*bool]    `json:"allow_register"`
		SignupCredit   Optional[*float64] `json:"signup_credit"`
		DefaultUserRPM Optional[*int]     `json:"default_user_rpm"`
		RequestTimeout Optional[*int]     `json:"request_timeout_seconds"`
		LogRetention   Optional[*int]     `json:"log_retention_days"`
		TrustProxy     Optional[*bool]    `json:"trust_proxy"`
		GitHub         *oauthAppPatch     `json:"github"`
		Google         *oauthAppPatch     `json:"google"`
	}
	if !decode(w, r, &in) {
		return
	}
	values := map[string]*string{}
	bad := func(msg string) {
		writeErr(w, http.StatusBadRequest, "invalid_request", msg)
	}
	if in.SiteName.Set {
		if v := in.SiteName.Value; v != nil {
			name := strings.TrimSpace(*v)
			if name == "" || len(name) > 100 {
				bad("site_name must be 1–100 characters")
				return
			}
			values[keySiteName] = &name
		} else {
			values[keySiteName] = nil
		}
	}
	if in.PublicURL.Set {
		if v := in.PublicURL.Value; v != nil {
			u := strings.TrimRight(strings.TrimSpace(*v), "/")
			if !validPublicURL(u) {
				bad("public_url must look like https://api.example.com, or be empty to derive it from requests")
				return
			}
			values[keyPublicURL] = &u
		} else {
			values[keyPublicURL] = nil
		}
	}
	setBool := func(key string, o Optional[*bool]) {
		if !o.Set {
			return
		}
		if o.Value == nil {
			values[key] = nil
		} else {
			values[key] = strPtr(strconv.FormatBool(*o.Value))
		}
	}
	setBool(keyAllowRegister, in.AllowRegister)
	setBool(keyTrustProxy, in.TrustProxy)
	if in.SignupCredit.Set {
		if v := in.SignupCredit.Value; v != nil {
			if *v < 0 || *v > 1e6 {
				bad("signup_credit must be between 0 and 1000000")
				return
			}
			values[keySignupCredit] = strPtr(strconv.FormatFloat(*v, 'f', -1, 64))
		} else {
			values[keySignupCredit] = nil
		}
	}
	for _, f := range []struct {
		key      string
		o        Optional[*int]
		min, max int
		name     string
	}{
		{keyDefaultUserRPM, in.DefaultUserRPM, 0, 1_000_000, "default_user_rpm"},
		{keyRequestTimeout, in.RequestTimeout, 5, 24 * 3600, "request_timeout_seconds"},
		{keyLogRetention, in.LogRetention, 0, 100 * 365, "log_retention_days"},
	} {
		if !f.o.Set {
			continue
		}
		if v := f.o.Value; v != nil {
			if *v < f.min || *v > f.max {
				bad(f.name + " is out of range")
				return
			}
			values[f.key] = strPtr(strconv.Itoa(*v))
		} else {
			values[f.key] = nil
		}
	}
	for _, p := range []struct {
		patch            *oauthAppPatch
		idKey, secretKey string
	}{{in.GitHub, keyGitHubID, keyGitHubSecret}, {in.Google, keyGoogleID, keyGoogleSecret}} {
		if p.patch == nil {
			continue
		}
		if p.patch.ClientID.Set {
			if v := p.patch.ClientID.Value; v == nil {
				values[p.idKey], values[p.secretKey] = nil, nil // back to the environment
			} else {
				values[p.idKey] = strPtr(strings.TrimSpace(*v)) // "" disables the provider
			}
		}
		if sec := p.patch.ClientSecret; sec != nil && strings.TrimSpace(*sec) != "" {
			values[p.secretKey] = strPtr(s.box.Encrypt(strings.TrimSpace(*sec)))
			if _, idSet := values[p.idKey]; !idSet {
				// Pin the current ID so the new secret isn't paired with an env ID later.
				id, _ := s.oauthCreds(strings.TrimSuffix(p.idKey, "_client_id"))
				values[p.idKey] = &id
			}
		}
	}

	// Don't let an admin lock everyone out by disabling every sign-in method.
	if err := s.checkSignInStillPossible(r.Context(), values); err != nil {
		writeErr(w, http.StatusBadRequest, "no_sign_in_provider", "at least one of GitHub or Google sign-in must stay configured")
		return
	}
	if err := s.store.SaveSettings(r.Context(), values); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.reloadSettings(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, s.settingsView(r))
}

// checkSignInStillPossible simulates applying values and reports
// errBadSetting if no sign-in provider would remain.
func (s *Server) checkSignInStillPossible(ctx context.Context, values map[string]*string) error {
	s.settingsMu.RLock()
	sim := make(map[string]string, len(s.settings))
	for k, v := range s.settings {
		sim[k] = v
	}
	s.settingsMu.RUnlock()
	for k, v := range values {
		if v == nil {
			delete(sim, k)
		} else {
			sim[k] = *v
		}
	}
	configured := func(idKey, secretKey, envID, envSecret string) bool {
		id, hasID := sim[idKey]
		if !hasID {
			return envID != "" && envSecret != ""
		}
		enc, hasSecret := sim[secretKey]
		return id != "" && hasSecret && enc != ""
	}
	if configured(keyGitHubID, keyGitHubSecret, s.cfg.GitHubClientID, s.cfg.GitHubClientSecret) ||
		configured(keyGoogleID, keyGoogleSecret, s.cfg.GoogleClientID, s.cfg.GoogleClientSecret) {
		return nil
	}
	return errBadSetting
}
