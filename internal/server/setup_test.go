package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/local4a/plexrouter/internal/config"
	"github.com/local4a/plexrouter/internal/gateway"
	"github.com/local4a/plexrouter/internal/oauth"
	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/server"
	"github.com/local4a/plexrouter/internal/store"
)

// A zero-config instance: no environment, no providers, no users.
func TestFirstRunSetupAndSettings(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Name: "PlexRouter", AllowRegister: true, DataDir: dir, RequestTimeout: 30 * time.Second}
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := secure.NewBox("s")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(cfg, st, box, gateway.New(st, box, gateway.Options{Timeout: cfg.RequestTimeout}, log), fstest.MapFS{}, "test", log)
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()
	env := &oauthEnv{t: t, base: hs.URL, cfg: cfg, st: st, srv: srv}
	c := env.browser()

	conf := c.must("GET", "/api/config", "", nil)
	if conf["setup_required"] != true || len(conf["oauth_providers"].([]any)) != 0 {
		t.Fatalf("fresh config = %v", conf)
	}
	info := c.must("GET", "/api/setup", "", nil)
	if get(info, "callback_urls", "github") != hs.URL+"/api/auth/oauth/github/callback" || info["suggested_public_url"] != hs.URL {
		t.Fatalf("setup info = %v", info)
	}
	if status, b := c.do("POST", "/api/setup", "", map[string]any{"site_name": "Acme AI"}); status != 400 {
		t.Errorf("setup without provider = %d %s", status, b)
	}
	if status, _ := c.do("POST", "/api/setup", "", map[string]any{"public_url": "ftp://x", "github": map[string]any{"client_id": "a", "client_secret": "b"}}); status != 400 {
		t.Errorf("bad public_url accepted: %d", status)
	}
	res := c.must("POST", "/api/setup", "", map[string]any{"site_name": "Acme AI",
		"github": map[string]any{"client_id": "gh-id-123", "client_secret": "gh-secret"}})
	if res["ok"] != true {
		t.Fatalf("setup = %v", res)
	}
	conf = c.must("GET", "/api/config", "", nil)
	if conf["name"] != "Acme AI" || conf["setup_required"] != true || len(conf["oauth_providers"].([]any)) != 1 {
		t.Fatalf("config after setup = %v", conf)
	}
	// The saved app is what sign-in uses.
	resp, err := c.http.Get(hs.URL + "/api/auth/oauth/github?mode=login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Host != "github.com" || loc.Query().Get("client_id") != "gh-id-123" {
		t.Fatalf("sign-in redirect = %s", loc)
	}

	// Complete the first sign-in through a fake GitHub: that person becomes admin.
	idp := newFakeIdP(t)
	srv.SetOAuthProvider(idp.provider(oauth.GitHub))
	idp.add("owner", fakeAccount{sub: "1", email: "owner@example.com", name: "Owner", verified: true})
	if to := env.signIn(c, "github", "login", "owner"); to != "/" {
		t.Fatalf("first sign-in → %q", to)
	}
	if get(c.must("GET", "/api/me", "", nil), "user", "role") != "admin" {
		t.Fatal("first sign-in after setup is not admin")
	}
	if conf := c.must("GET", "/api/config", "", nil); conf["setup_required"] != false {
		t.Errorf("setup still required: %v", conf)
	}
	if status, b := env.browser().do("POST", "/api/setup", "", map[string]any{"github": map[string]any{"client_id": "x", "client_secret": "y"}}); status != 403 || !strings.Contains(string(b), "setup_done") {
		t.Errorf("setup after admin = %d %s", status, b)
	}
	idp.add("second", fakeAccount{sub: "2", email: "second@example.com", name: "Second", verified: true})
	other := env.browser()
	env.signIn(other, "github", "login", "second")
	if get(other.must("GET", "/api/me", "", nil), "user", "role") != "user" {
		t.Error("second sign-in became admin")
	}

	// System settings: values, sources, and that the gateway picks them up.
	set := c.must("GET", "/api/admin/settings", "", nil)
	if get(set, "settings", "site_name") != "Acme AI" || get(set, "sources", "site_name") != "console" ||
		get(set, "sources", "default_user_rpm") != "default" || get(set, "settings", "github", "client_secret_set") != true ||
		get(set, "settings", "github", "client_id") != "gh-id-123" {
		t.Fatalf("settings = %v", set)
	}
	set = c.must("PATCH", "/api/admin/settings", "", map[string]any{"default_user_rpm": 1, "allow_register": false,
		"log_retention_days": 7, "request_timeout_seconds": 60, "trust_proxy": true, "signup_credit": 0.5})
	if get(set, "settings", "default_user_rpm") != 1.0 || get(set, "settings", "allow_register") != false ||
		get(set, "settings", "log_retention_days") != 7.0 || srv.LogRetentionDays() != 7 {
		t.Fatalf("patched settings = %v", set)
	}
	if status, _ := c.do("PATCH", "/api/admin/settings", "", map[string]any{"request_timeout_seconds": 1}); status != 400 {
		t.Errorf("out-of-range timeout accepted: %d", status)
	}
	// Disabling the only sign-in method is refused.
	if status, b := c.do("PATCH", "/api/admin/settings", "", map[string]any{"github": map[string]any{"client_id": ""}}); status != 400 || !strings.Contains(string(b), "no_sign_in_provider") {
		t.Errorf("lockout = %d %s", status, b)
	}
	// A new secret is stored without being echoed back.
	set = c.must("PATCH", "/api/admin/settings", "", map[string]any{"google": map[string]any{"client_id": "go-id", "client_secret": "go-secret"}})
	if get(set, "settings", "google", "client_id") != "go-id" || strings.Contains(fmtJSON(set), "go-secret") {
		t.Errorf("google settings = %v", set)
	}
	if conf := c.must("GET", "/api/config", "", nil); conf["allow_register"] != false || len(conf["oauth_providers"].([]any)) != 2 {
		t.Errorf("config after settings = %v", conf)
	}
	// Closed registration now applies.
	idp.add("third", fakeAccount{sub: "3", email: "third@example.com", name: "Third", verified: true})
	if to := env.signIn(env.browser(), "github", "login", "third"); to != "/login?oauth_error=registration_closed" {
		t.Errorf("closed registration → %q", to)
	}
	// Reset returns to the default.
	set = c.must("PATCH", "/api/admin/settings", "", map[string]any{"allow_register": nil})
	if get(set, "settings", "allow_register") != true || get(set, "sources", "allow_register") != "default" {
		t.Errorf("reset = %v", set)
	}

	// The default per-user RPM reaches the gateway.
	up := newKeyedUpstream(t)
	pid := get(c.must("POST", "/api/admin/providers", "", map[string]any{"name": "Up", "type": "openai_compatible", "base_url": up.URL + "/v1", "api_key": "good"}), "provider", "id")
	c.must("POST", "/api/admin/models", "", map[string]any{"slug": "m/free", "input_price": 0, "output_price": 0,
		"endpoints": []map[string]any{{"provider_id": pid, "upstream_model": "gpt-test"}}})
	key := other.must("POST", "/api/keys", "", map[string]any{"name": "k"})["secret"].(string)
	body := map[string]any{"model": "m/free", "messages": []map[string]any{{"role": "user", "content": "hi"}}}
	if s1, b := other.do("POST", "/v1/chat/completions", key, body); s1 != 200 {
		t.Fatalf("first request = %d %s", s1, b)
	}
	if s2, _ := other.do("POST", "/v1/chat/completions", key, body); s2 != http.StatusTooManyRequests {
		t.Errorf("second request = %d, want 429 from default_user_rpm=1", s2)
	}
}

func fmtJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
