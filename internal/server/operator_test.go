package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// keyedUpstream is an OpenAI-compatible server whose behaviour depends on the
// API key, to exercise key rotation and failure handling.
type keyedUpstream struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newKeyedUpstream(t *testing.T) *keyedUpstream {
	u := &keyedUpstream{hits: map[string]int{}}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		u.mu.Lock()
		u.hits[key]++
		u.mu.Unlock()
		switch key {
		case "revoked":
			http.Error(w, `{"error":{"message":"Incorrect API key provided","code":"invalid_api_key"}}`, http.StatusUnauthorized)
			return
		case "broke":
			http.Error(w, `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","code":"insufficient_quota"}}`, http.StatusTooManyRequests)
			return
		case "slow":
			w.Header().Set("Retry-After", "120")
			http.Error(w, `{"error":{"message":"Rate limit reached","code":"rate_limit_exceeded"}}`, http.StatusTooManyRequests)
			return
		case "down":
			http.Error(w, `{"error":{"message":"internal error"}}`, http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"gpt-test"},{"id":"claude-x-20250101"}]}`)
		case "/v1/chat/completions":
			io.WriteString(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *keyedUpstream) count(key string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits[key]
}

func fakeCatalog(t *testing.T) string {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[
			{"id":"openai/gpt-test","name":"GPT Test","description":"d","context_length":128000,"pricing":{"prompt":"0.000001","completion":"0.000002"}},
			{"id":"anthropic/claude-x","name":"Claude X","context_length":200000,"pricing":{"prompt":"0.000003","completion":"0.000015"}},
			{"id":"vendor/auto","name":"Auto","pricing":{"prompt":"-1","completion":"-1"}}]}`)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

func TestOperatorFeatures(t *testing.T) {
	up := newKeyedUpstream(t)
	c, env, idp := setup(t)
	me := c.must("GET", "/api/me", "", nil)
	c.must("POST", fmt.Sprintf("/api/admin/users/%v/credits", get(me, "user", "id")), "", map[string]any{"amount": 10})

	// Provider with a pool of keys; the first three fail in different ways.
	p1 := get(c.must("POST", "/api/admin/providers", "", map[string]any{
		"name": "Pool", "type": "openai_compatible", "base_url": up.URL + "/v1", "api_key": "revoked"}), "provider", "id")
	added := c.must("POST", fmt.Sprintf("/api/admin/providers/%v/keys", p1), "", map[string]any{
		"api_keys": []string{"broke", " slow ", "", "good-1"}, "name": "extra"})["data"].([]any)
	if len(added) != 3 || added[0].(map[string]any)["name"] != "extra #1" {
		t.Fatalf("added keys = %v", added)
	}
	p2 := get(c.must("POST", "/api/admin/providers", "", map[string]any{
		"name": "Flaky", "type": "openai_compatible", "base_url": up.URL + "/v1", "api_key": "down"}), "provider", "id")

	// Sell at $2/$4 per 1M; pay $1/$2.
	c.must("POST", "/api/admin/models", "", map[string]any{"slug": "m/one", "input_price": 2, "output_price": 4,
		"endpoints": []map[string]any{{"provider_id": p1, "upstream_model": "gpt-test", "input_cost": 1, "output_cost": 2}}})
	c.must("POST", "/api/admin/models", "", map[string]any{"slug": "m/two", "input_price": 2, "output_price": 4,
		"endpoints": []map[string]any{
			{"provider_id": p2, "upstream_model": "gpt-test", "priority": 0},
			{"provider_id": p1, "upstream_model": "gpt-test", "priority": 1, "input_cost": 1, "output_cost": 2}}})

	key := c.must("POST", "/api/keys", "", map[string]any{"name": "k"})["secret"].(string)
	chat := func(k, model string, extra ...map[string]any) (int, string) {
		body := map[string]any{"model": model, "messages": []map[string]any{{"role": "user", "content": "hi"}}}
		for _, e := range extra {
			for kk, v := range e {
				body[kk] = v
			}
		}
		status, b := c.do("POST", "/v1/chat/completions", k, body)
		return status, string(b)
	}

	// One request walks the pool: 401 → disabled, quota → disabled,
	// 429 → cooling down, then the good key answers.
	if status, b := chat(key, "m/one"); status != 200 {
		t.Fatalf("chat through pool = %d %s", status, b)
	}
	keys := c.must("GET", fmt.Sprintf("/api/admin/providers/%v/keys", p1), "", nil)["data"].([]any)
	statuses := map[string]string{}
	var revokedID any
	for _, k := range keys {
		k := k.(map[string]any)
		statuses[k["name"].(string)] = k["status"].(string)
		if k["name"] == "Default" {
			revokedID = k["id"]
			if !strings.Contains(k["last_error"].(string), "Incorrect API key") || k["key_hint"] == "revoked" {
				t.Errorf("revoked key = %v", k)
			}
		}
	}
	want := map[string]string{"Default": "auth_failed", "extra #1": "quota_exceeded", "extra #2": "cooling_down", "extra #3": "active"}
	if fmt.Sprint(statuses) != fmt.Sprint(want) {
		t.Errorf("key statuses = %v, want %v", statuses, want)
	}
	// Failed keys are skipped from now on.
	chat(key, "m/one")
	if up.count("revoked") != 1 || up.count("broke") != 1 || up.count("slow") != 1 || up.count("good-1") != 2 {
		t.Errorf("hits = %v", up.hits)
	}

	// Provider view and alerts reflect the pool.
	var prov map[string]any
	for _, p := range c.must("GET", "/api/admin/providers", "", nil)["data"].([]any) {
		if p.(map[string]any)["id"] == p1 {
			prov = p.(map[string]any)
		}
	}
	if prov["keys_total"] != 4.0 || prov["keys_active"] != 1.0 || get(prov, "health", "status") != "idle" {
		t.Errorf("provider view = %v", prov)
	}
	stats := c.must("GET", "/api/admin/stats", "", nil)
	kinds := []string{}
	for _, a := range stats["alerts"].([]any) {
		kinds = append(kinds, a.(map[string]any)["kind"].(string))
	}
	if strings.Count(strings.Join(kinds, ","), "key_failed") != 2 {
		t.Errorf("alerts = %v", stats["alerts"])
	}

	// Re-enabling a failed key puts it back.
	k := c.must("PATCH", fmt.Sprintf("/api/admin/providers/%v/keys/%v", p1, revokedID), "", map[string]any{"enabled": true})
	if get(k, "key", "status") != "active" || get(k, "key", "last_error") != "" {
		t.Errorf("re-enabled key = %v", k)
	}

	// Circuit breaker: after 3 failures the flaky endpoint is skipped.
	for i := 0; i < 3; i++ {
		if status, b := chat(key, "m/two"); status != 200 {
			t.Fatalf("fallback chat = %d %s", status, b)
		}
	}
	chat(key, "m/two")
	if up.count("down") != 3 {
		t.Errorf("open circuit still tried: down hits = %d", up.count("down"))
	}
	var open any
	for _, m := range c.must("GET", "/api/admin/models", "", nil)["data"].([]any) {
		if m.(map[string]any)["slug"] == "m/two" {
			open = m.(map[string]any)["endpoints"].([]any)[0].(map[string]any)["circuit_open_until"]
		}
	}
	if open == nil {
		t.Error("circuit_open_until not reported")
	}

	// Profit: 2 + 4 successful requests with cost known on every endpoint that served.
	rep := c.must("GET", "/api/admin/report?days=7", "", nil)
	tot := rep["totals"].(map[string]any)
	revenue, cost := 6*(10*2+5*4)/1e6, 6*(10*1+5*2)/1e6
	if !approx(tot["revenue"].(float64), revenue) || !approx(tot["upstream_cost"].(float64), cost) ||
		!approx(tot["profit"].(float64), revenue-cost) || tot["unknown_cost_requests"] != 0.0 {
		t.Errorf("report totals = %v", tot)
	}
	if len(rep["daily"].([]any)) != 7 || len(rep["by_provider"].([]any)) != 2 {
		t.Errorf("report = %v", rep)
	}
	logs := c.must("GET", "/api/admin/logs?page_size=1", "", nil)["data"].([]any)
	if l := logs[0].(map[string]any); l["provider_key"] == "" || !approx(l["upstream_cost"].(float64), (10*1+5*2)/1e6) {
		t.Errorf("admin log = %v", l)
	}
	if _, leaked := c.must("GET", "/api/usage/logs?page_size=1", "", nil)["data"].([]any)[0].(map[string]any)["upstream_cost"]; leaked {
		t.Error("upstream cost leaked to users")
	}

	// Per-key rate limit.
	limited := c.must("POST", "/api/keys", "", map[string]any{"name": "rl", "rate_limit_rpm": 2})["secret"].(string)
	chat(limited, "m/one")
	chat(limited, "m/one")
	req, _ := http.NewRequest("POST", c.base+"/v1/chat/completions", strings.NewReader(`{"model":"m/one","messages":[{"role":"user","content":"x"}]}`))
	req.Header.Set("Authorization", "Bearer "+limited)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Errorf("rate limit: %v %v", resp.StatusCode, err)
	}

	// Per-user rate limit set by an admin.
	idp.add("carol", fakeAccount{sub: "3", email: "carol@example.com", name: "Carol", verified: true})
	carol := env.browser()
	env.signIn(carol, "github", "login", "carol")
	carolID := get(carol.must("GET", "/api/me", "", nil), "user", "id")
	c.must("POST", fmt.Sprintf("/api/admin/users/%v/credits", carolID), "", map[string]any{"amount": 1})
	u := c.must("PATCH", fmt.Sprintf("/api/admin/users/%v", carolID), "", map[string]any{"rate_limit_rpm": 1})
	if get(u, "user", "rate_limit_rpm") != 1.0 {
		t.Errorf("user rpm = %v", u)
	}
	carolKey := carol.must("POST", "/api/keys", "", map[string]any{"name": "c"})["secret"].(string)
	if s1, _ := chat(carolKey, "m/one"); s1 != 200 {
		t.Errorf("first carol request = %d", s1)
	}
	if s2, _ := chat(carolKey, "m/one"); s2 != 429 {
		t.Errorf("second carol request = %d, want 429", s2)
	}

	// Allowed models: a disallowed model is refused, a disallowed primary falls
	// back to an allowed one.
	only := c.must("POST", "/api/keys", "", map[string]any{"name": "only", "allowed_models": []string{"m/one", " ", "m/one"}})
	if fmt.Sprint(get(only, "key", "allowed_models")) != "[m/one]" {
		t.Errorf("allowed_models = %v", get(only, "key", "allowed_models"))
	}
	onlyKey := only["secret"].(string)
	if status, b := chat(onlyKey, "m/two"); status != 403 || !strings.Contains(b, "model_not_allowed") {
		t.Errorf("disallowed model = %d %s", status, b)
	}
	if status, b := chat(onlyKey, "m/two", map[string]any{"models": []string{"m/one"}}); status != 200 || !strings.Contains(b, `"model":"m/one"`) {
		t.Errorf("fallback to allowed model = %d %s", status, b)
	}

	// Catalog import: match upstream names, apply markup, set costs.
	env.srv.SetCatalogURL(fakeCatalog(t))
	cat := c.must("GET", fmt.Sprintf("/api/admin/catalog?provider_id=%v", p1), "", nil)["data"].([]any)
	matches := map[string]any{}
	for _, m := range cat {
		m := m.(map[string]any)
		matches[m["id"].(string)] = m["upstream_match"]
	}
	if len(cat) != 2 || matches["openai/gpt-test"] != "gpt-test" || matches["anthropic/claude-x"] != "claude-x-20250101" {
		t.Fatalf("catalog = %v", cat)
	}
	imp := c.must("POST", "/api/admin/catalog/import", "", map[string]any{"provider_id": p1, "markup_percent": 50, "enable": true,
		"items": []map[string]any{{"id": "openai/gpt-test", "upstream_model": "gpt-test"}, {"id": "anthropic/claude-x", "upstream_model": "claude-x-20250101"}}})
	if imp["created"] != 2.0 {
		t.Fatalf("import = %v", imp)
	}
	for _, m := range c.must("GET", "/api/models", "", nil)["data"].([]any) {
		m := m.(map[string]any)
		if m["id"] == "openai/gpt-test" && (m["input_price"] != 1.5 || m["output_price"] != 3.0 || m["context_length"] != 128000.0) {
			t.Errorf("imported model = %v", m)
		}
	}
	if status, b := chat(key, "openai/gpt-test"); status != 200 {
		t.Errorf("imported model chat = %d %s", status, b)
	}
	again := c.must("POST", "/api/admin/catalog/import", "", map[string]any{"provider_id": p2, "markup_percent": 10, "enable": true,
		"update_existing": true, "items": []map[string]any{{"id": "openai/gpt-test", "upstream_model": "gpt-test"}}})
	if again["created"] != 0.0 || again["updated"] != 1.0 || again["endpoints_added"] != 1.0 {
		t.Errorf("re-import = %v", again)
	}

	// Log retention.
	n, err := env.st.PurgeLogs(context.Background(), time.Now().Add(time.Hour))
	if err != nil || n == 0 {
		t.Errorf("purge = %d %v", n, err)
	}
}
