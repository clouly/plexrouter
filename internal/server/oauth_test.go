package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
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

type fakeAccount struct {
	sub, email, name string
	verified         bool
}

// fakeIdP plays both Google (OpenID userinfo) and GitHub (/user, /user/emails).
// The authorization code is the account key; the access token echoes it.
type fakeIdP struct {
	*httptest.Server
	mu       sync.Mutex
	accounts map[string]fakeAccount
}

func newFakeIdP(t *testing.T) *fakeIdP {
	f := &fakeIdP{accounts: map[string]fakeAccount{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "csecret" {
				http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
				return
			}
			if r.Form.Get("code_verifier") == "" {
				t.Errorf("token request without PKCE verifier")
			}
			if !strings.HasSuffix(r.Form.Get("redirect_uri"), "/callback") {
				t.Errorf("redirect_uri = %q", r.Form.Get("redirect_uri"))
			}
			code := r.Form.Get("code")
			if _, ok := f.accounts[code]; !ok {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, code)
			return
		}
		acct, ok := f.accounts[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/userinfo":
			json.NewEncoder(w).Encode(map[string]any{"sub": acct.sub, "email": acct.email, "email_verified": acct.verified, "name": acct.name})
		case "/user":
			id := 0
			fmt.Sscan(acct.sub, &id)
			json.NewEncoder(w).Encode(map[string]any{"id": id, "login": acct.name, "name": ""})
		case "/user/emails":
			json.NewEncoder(w).Encode([]map[string]any{
				{"email": "noreply@users.example", "primary": false, "verified": true},
				{"email": acct.email, "primary": true, "verified": acct.verified},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeIdP) add(code string, a fakeAccount) {
	f.mu.Lock()
	f.accounts[code] = a
	f.mu.Unlock()
}

func (f *fakeIdP) provider(name string) *oauth.Provider {
	p := &oauth.Provider{Name: name, ClientID: "cid", ClientSecret: "csecret",
		AuthURL: f.URL + "/authorize", TokenURL: f.URL + "/token", UserInfoURL: f.URL}
	if name == oauth.Google {
		p.UserInfoURL = f.URL + "/userinfo"
	}
	return p
}

type oauthEnv struct {
	t    *testing.T
	base string
	cfg  *config.Config
	st   *store.Store
	srv  *server.Server
}

func newOAuthEnv(t *testing.T, idp *fakeIdP) *oauthEnv {
	dir := t.TempDir()
	cfg := &config.Config{Name: "Test", AllowRegister: true, DataDir: dir, RequestTimeout: 30 * time.Second}
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secure.NewBox("test-secret")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	web := fstest.MapFS{"index.html": {Data: []byte("<html>console</html>")}}
	srv := server.New(cfg, st, box, gateway.New(st, box, gateway.Options{Timeout: cfg.RequestTimeout}, log), web, "test", log)
	srv.SetOAuthProvider(idp.provider(oauth.Google))
	srv.SetOAuthProvider(idp.provider(oauth.GitHub))
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return &oauthEnv{t: t, base: hs.URL, cfg: cfg, st: st, srv: srv}
}

// browser is a cookie-keeping client that does not follow redirects.
func (e *oauthEnv) browser() *client {
	jar, _ := cookiejar.New(nil)
	c := newClient(e.t, e.base)
	c.http = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return c
}

// signIn runs the redirect dance and returns where the server sent the
// browser at the end (e.g. "/" or "/login?oauth_error=…").
func (e *oauthEnv) signIn(c *client, provider, mode, code string) string {
	e.t.Helper()
	resp, err := c.http.Get(e.base + "/api/auth/oauth/" + provider + "?mode=" + mode)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		e.t.Fatalf("start status %d", resp.StatusCode)
	}
	auth, _ := url.Parse(resp.Header.Get("Location"))
	q := auth.Query()
	if !strings.HasPrefix(auth.String(), strings.TrimSuffix(e.base, "/")) && q.Get("state") != "" {
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("client_id") != "cid" {
			e.t.Fatalf("authorize URL missing PKCE/client: %s", auth)
		}
	} else {
		return resp.Header.Get("Location") // refused before reaching the provider
	}
	cb := q.Get("redirect_uri") + "?state=" + url.QueryEscape(q.Get("state"))
	if code == "" {
		cb += "&error=access_denied"
	} else {
		cb += "&code=" + url.QueryEscape(code)
	}
	resp, err = c.http.Get(cb)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		e.t.Fatalf("callback status %d", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

func (e *oauthEnv) me(c *client) map[string]any {
	return c.must("GET", "/api/me", "", nil)["user"].(map[string]any)
}

func TestOAuth(t *testing.T) {
	idp := newFakeIdP(t)
	e := newOAuthEnv(t, idp)
	idp.add("alice-g", fakeAccount{sub: "g-alice", email: "Alice@Example.com", name: "Alice", verified: true})
	idp.add("alice-gh", fakeAccount{sub: "101", email: "alice@example.com", name: "alice-gh", verified: true})
	idp.add("bob-gh", fakeAccount{sub: "202", email: "bob@example.com", name: "bobby", verified: true})
	idp.add("eve-g", fakeAccount{sub: "g-eve", email: "eve@example.com", name: "Eve", verified: false})
	idp.add("carol-g", fakeAccount{sub: "g-carol", email: "carol@example.com", name: "Carol", verified: true})
	idp.add("bob-g", fakeAccount{sub: "g-bob", email: "bob@example.com", name: "Bob", verified: true})
	idp.add("dave-g", fakeAccount{sub: "g-dave", email: "dave@example.com", name: "Dave", verified: true})

	cfg := map[string]any{}
	json.Unmarshal(must(t, e.browser(), "GET", "/api/config"), &cfg)
	if fmt.Sprint(cfg["oauth_providers"]) != "[google github]" {
		t.Fatalf("oauth_providers = %v", cfg["oauth_providers"])
	}

	// New account via Google: the first user becomes admin.
	alice := e.browser()
	if to := e.signIn(alice, "google", "login", "alice-g"); to != "/" {
		t.Fatalf("google sign-in redirected to %q", to)
	}
	me := e.me(alice)
	if me["email"] != "alice@example.com" || me["role"] != "admin" || me["name"] != "Alice" {
		t.Fatalf("me = %v", me)
	}
	aliceID := me["id"]

	// Same Google account again → same user.
	again := e.browser()
	e.signIn(again, "google", "login", "alice-g")
	if e.me(again)["id"] != aliceID {
		t.Fatal("second Google sign-in made a new user")
	}

	// GitHub with the same verified email → linked to Alice automatically.
	gh := e.browser()
	if to := e.signIn(gh, "github", "login", "alice-gh"); to != "/" {
		t.Fatalf("github sign-in redirected to %q", to)
	}
	if e.me(gh)["id"] != aliceID {
		t.Fatal("verified email did not link to the existing account")
	}
	ids := alice.must("GET", "/api/me/identities", "", nil)["data"].([]any)
	if len(ids) != 2 {
		t.Fatalf("identities = %v", ids)
	}

	// Unverified email is refused; a cancelled consent is reported.
	if to := e.signIn(e.browser(), "google", "login", "eve-g"); to != "/login?oauth_error=email_unverified" {
		t.Errorf("unverified → %q", to)
	}
	if to := e.signIn(e.browser(), "google", "login", ""); to != "/login?oauth_error=access_denied" {
		t.Errorf("cancel → %q", to)
	}

	// A forged callback (no attempt cookie) fails the state check.
	forged := e.browser()
	resp, _ := forged.http.Get(e.base + "/api/auth/oauth/google/callback?state=x&code=alice-g")
	if loc := resp.Header.Get("Location"); loc != "/login?oauth_error=state_mismatch" {
		t.Errorf("forged callback → %q", loc)
	}
	if to := e.signIn(e.browser(), "google", "link", "carol-g"); to != "/login?oauth_error=not_logged_in" {
		t.Errorf("link without session → %q", to)
	}

	// Bob signs up with Google, links GitHub from settings, then unlinks it.
	bob := e.browser()
	if to := e.signIn(bob, "google", "login", "bob-g"); to != "/" {
		t.Fatalf("bob sign-in → %q", to)
	}
	if e.me(bob)["role"] != "user" {
		t.Errorf("second user role = %v", e.me(bob)["role"])
	}
	if to := e.signIn(bob, "github", "link", "bob-gh"); to != "/settings?linked=github" {
		t.Fatalf("link → %q", to)
	}
	if ids := bob.must("GET", "/api/me/identities", "", nil)["data"].([]any); len(ids) != 2 {
		t.Fatalf("bob identities = %v", ids)
	}
	// Bob can't take over Alice's Google account.
	if to := e.signIn(bob, "google", "link", "alice-g"); to != "/settings?oauth_error=already_linked" {
		t.Errorf("link someone else's account → %q", to)
	}
	bob.must("DELETE", "/api/me/identities/github", "", nil)

	// The last sign-in method can't be removed.
	alice.must("DELETE", "/api/me/identities/github", "", nil)
	if status, b := alice.do("DELETE", "/api/me/identities/google", "", nil); status != 400 || !strings.Contains(string(b), "last_login_method") {
		t.Errorf("unlink last method = %d %s", status, b)
	}
	// Password endpoints are gone.
	if status, _ := e.browser().do("POST", "/api/auth/login", "", map[string]any{"email": "alice@example.com", "password": "x"}); status != 404 {
		t.Errorf("password login status = %d", status)
	}

	// Closed registration blocks new accounts but not existing ones.
	e.cfg.AllowRegister = false
	if to := e.signIn(e.browser(), "google", "login", "carol-g"); to != "/login?oauth_error=registration_closed" {
		t.Errorf("closed registration → %q", to)
	}
	if to := e.signIn(e.browser(), "google", "login", "alice-g"); to != "/" {
		t.Errorf("existing user with closed registration → %q", to)
	}

	// Listed admin emails may always sign up and become admins; existing
	// listed users are promoted on their next sign-in.
	e.cfg.AdminEmails = []string{"carol@example.com", "bob@example.com"}
	carol := e.browser()
	if to := e.signIn(carol, "google", "login", "carol-g"); to != "/" || e.me(carol)["role"] != "admin" {
		t.Errorf("listed admin sign-up → %q, role %v", to, e.me(carol)["role"])
	}
	bob2 := e.browser()
	e.signIn(bob2, "google", "login", "bob-g")
	if e.me(bob2)["role"] != "admin" {
		t.Errorf("listed existing user not promoted: %v", e.me(bob2)["role"])
	}
	e.cfg.AllowRegister = true
	dave := e.browser()
	e.signIn(dave, "google", "login", "dave-g")
	if e.me(dave)["role"] != "user" {
		t.Errorf("unlisted user with an admin list = %v", e.me(dave)["role"])
	}

	// Disabled accounts can't sign in.
	disabled := true
	e.st.UpdateUser(context.Background(), int64(e.me(bob2)["id"].(float64)), store.UserUpdate{Disabled: &disabled})
	if to := e.signIn(e.browser(), "google", "login", "bob-g"); to != "/login?oauth_error=account_disabled" {
		t.Errorf("disabled → %q", to)
	}
}

func must(t *testing.T, c *client, method, path string) []byte {
	t.Helper()
	status, b := c.do(method, path, "", nil)
	if status != 200 {
		t.Fatalf("%s %s = %d", method, path, status)
	}
	return b
}
