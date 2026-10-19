package server_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clouly/plexrouter/internal/gateway"
)

// --- fake upstreams ---

func fakeOpenAI(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oa-key" {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"gpt-test"},{"id":"gpt-other"}]}`)
		case "/v1/chat/completions":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "gpt-test" {
				t.Errorf("openai upstream got model %v", body["model"])
			}
			if _, leaked := body["models"]; leaked {
				t.Errorf("gateway field 'models' leaked upstream")
			}
			if body["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, part := range []string{"Hel", "lo"} {
					fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", part)
				}
				io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
				io.WriteString(w, "data: [DONE]\n\n")
				return
			}
			io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		case "/v1/embeddings":
			io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"gpt-test","usage":{"prompt_tokens":8,"total_tokens":8}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func fakeAnthropic(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "an-key" || r.Header.Get("anthropic-version") == "" {
			http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model     string            `json:"model"`
			System    string            `json:"system"`
			MaxTokens int               `json:"max_tokens"`
			Stream    bool              `json:"stream"`
			Messages  []json.RawMessage `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.System != "be brief" || body.MaxTokens == 0 || len(body.Messages) != 1 {
			t.Errorf("anthropic upstream got bad body: %+v", body)
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			events := []string{
				`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":12,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"ping"}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":6}}`,
				`{"type":"message_stop"}`,
			}
			for _, e := range events {
				var typ struct{ Type string }
				json.Unmarshal([]byte(e), &typ)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ.Type, e)
			}
			return
		}
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":6}}`)
	}))
}

func fakeGemini(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "ge-key" {
			http.Error(w, `{"error":{"code":403,"message":"bad key"}}`, http.StatusForbidden)
			return
		}
		var body struct {
			Contents          []json.RawMessage `json:"contents"`
			SystemInstruction struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"systemInstruction"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Contents) != 1 || len(body.SystemInstruction.Parts) != 1 {
			t.Errorf("gemini upstream got bad body: %+v", body)
		}
		switch r.URL.Path {
		case "/v1beta/models/gemini-test:generateContent":
			io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":4,"totalTokenCount":11}}`)
		case "/v1beta/models/gemini-test:streamGenerateContent":
			if r.URL.Query().Get("alt") != "sse" {
				t.Errorf("gemini stream without alt=sse")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hel\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":1}}\r\n\r\n")
			io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":4}}\r\n\r\n")
		default:
			http.NotFound(w, r)
		}
	}))
}

func failing() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
	}))
}

// --- harness ---

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, auth string, body any) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// must performs a request, requires 200, and decodes the JSON response.
func (c *client) must(method, path, auth string, body any) map[string]any {
	c.t.Helper()
	status, b := c.do(method, path, auth, body)
	if status != http.StatusOK {
		c.t.Fatalf("%s %s: status %d: %s", method, path, status, b)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		c.t.Fatalf("%s %s: bad JSON %q", method, path, b)
	}
	return out
}

func get(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		v = v.(map[string]any)[p]
	}
	return v
}

// setup starts a server whose console sign-in goes through a fake GitHub,
// and signs in the first user (the admin).
func setup(t *testing.T) (*client, *oauthEnv, *fakeIdP) {
	idp := newFakeIdP(t)
	env := newOAuthEnv(t, idp)
	idp.add("admin", fakeAccount{sub: "1", email: "admin@example.com", name: "Admin", verified: true})
	c := env.browser()
	if to := env.signIn(c, "github", "login", "admin"); to != "/" {
		t.Fatalf("admin sign-in → %q", to)
	}
	return c, env, idp
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

type streamResult struct {
	text     string
	usage    map[string]any
	model    string
	gotDone  bool
	finished string
}

func readStream(t *testing.T, base, key string, body any) streamResult {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream status %d: %s", resp.StatusCode, msg)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	var res streamResult
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		if line == "[DONE]" {
			res.gotDone = true
			continue
		}
		var ch map[string]any
		if err := json.Unmarshal([]byte(line), &ch); err != nil {
			t.Fatalf("bad chunk %q", line)
		}
		if e, ok := ch["error"]; ok {
			t.Fatalf("stream error: %v", e)
		}
		res.model, _ = ch["model"].(string)
		if u, ok := ch["usage"].(map[string]any); ok {
			res.usage = u
		}
		for _, c := range ch["choices"].([]any) {
			c := c.(map[string]any)
			if d, ok := c["delta"].(map[string]any); ok {
				s, _ := d["content"].(string)
				res.text += s
			}
			if f, ok := c["finish_reason"].(string); ok {
				res.finished = f
			}
		}
	}
	return res
}

// --- the test ---

func TestEndToEnd(t *testing.T) {
	oa, an, ge, bad := fakeOpenAI(t), fakeAnthropic(t), fakeGemini(t), failing()
	defer oa.Close()
	defer an.Close()
	defer ge.Close()
	defer bad.Close()
	c, env, idp := setup(t)

	// The first user to sign in is admin.
	me := c.must("GET", "/api/me", "", nil)
	if get(me, "user", "role") != "admin" {
		t.Fatalf("first user should be admin: %v", me)
	}
	userID := get(me, "user", "id").(float64)

	newProvider := func(name, typ, base, key string) float64 {
		p := c.must("POST", "/api/admin/providers", "", map[string]any{"name": name, "type": typ, "base_url": base, "api_key": key})
		if hint := get(p, "provider", "api_key_hint").(string); strings.Contains(hint, key) && len(key) > 6 {
			t.Errorf("api key not masked: %q", hint)
		}
		return get(p, "provider", "id").(float64)
	}
	pOA := newProvider("OpenAI", "openai_compatible", oa.URL+"/v1", "oa-key")
	pAN := newProvider("Anthropic", "anthropic", an.URL, "an-key")
	pGE := newProvider("Gemini", "gemini", ge.URL, "ge-key")
	pBad := newProvider("Broken", "openai_compatible", bad.URL+"/v1", "x")

	// Provider test and upstream model listing.
	if res := c.must("POST", fmt.Sprintf("/api/admin/providers/%v/test", pOA), "", nil); res["ok"] != true {
		t.Fatalf("provider test failed: %v", res)
	}
	if res := c.must("POST", fmt.Sprintf("/api/admin/providers/%v/test", pBad), "", nil); res["ok"] != false {
		t.Fatalf("broken provider test should fail: %v", res)
	}
	ups := c.must("GET", fmt.Sprintf("/api/admin/providers/%v/upstream-models", pOA), "", nil)
	if n := len(ups["data"].([]any)); n != 2 {
		t.Fatalf("upstream models: %v", ups)
	}
	imp := c.must("POST", "/api/admin/models/import", "", map[string]any{"provider_id": pOA, "upstream_models": []string{"gpt-other"}, "slug_prefix": "oa"})
	if imp["created"] != 1.0 {
		t.Fatalf("import: %v", imp)
	}

	// $2 / $10 per 1M tokens.
	newModel := func(slug string, eps ...map[string]any) {
		c.must("POST", "/api/admin/models", "", map[string]any{
			"slug": slug, "name": slug, "input_price": 2, "output_price": 10, "context_length": 128000, "endpoints": eps,
		})
	}
	ep := func(p float64, up string, prio int) map[string]any {
		return map[string]any{"provider_id": p, "upstream_model": up, "priority": prio}
	}
	newModel("test/openai", ep(pOA, "gpt-test", 0))
	newModel("test/claude", ep(pAN, "claude-test", 0))
	newModel("test/gemini", ep(pGE, "gemini-test", 0))
	newModel("test/fallback", ep(pBad, "whatever", 0), ep(pOA, "gpt-test", 1))
	newModel("test/broken", ep(pBad, "whatever", 0))

	pub := c.must("GET", "/api/models", "", nil)
	if n := len(pub["data"].([]any)); n != 5 { // imported model is disabled
		t.Fatalf("public models = %d: %v", n, pub)
	}

	keyRes := c.must("POST", "/api/keys", "", map[string]any{"name": "test"})
	key := keyRes["secret"].(string)
	if !strings.HasPrefix(key, gateway.KeyPrefix) {
		t.Fatalf("key %q", key)
	}

	msgs := []map[string]any{{"role": "system", "content": "be brief"}, {"role": "user", "content": "hi"}}

	// No credits yet: paid model is refused.
	if status, b := c.do("POST", "/v1/chat/completions", key, map[string]any{"model": "test/openai", "messages": msgs}); status != http.StatusPaymentRequired {
		t.Fatalf("expected 402 without credits, got %d %s", status, b)
	}

	c.must("POST", fmt.Sprintf("/api/admin/users/%v/credits", userID), "", map[string]any{"amount": 10, "note": "test"})

	wantCost := func(prompt, completion float64) float64 { return (prompt*2 + completion*10) / 1e6 }
	var spent float64

	cases := []struct {
		slug               string
		prompt, completion float64
	}{
		{"test/openai", 10, 5},
		{"test/claude", 12, 6},
		{"test/gemini", 7, 4},
		{"test/fallback", 10, 5},
	}
	for _, tc := range cases {
		t.Run("chat "+tc.slug, func(t *testing.T) {
			res := c.must("POST", "/v1/chat/completions", key, map[string]any{"model": tc.slug, "messages": msgs})
			choice := res["choices"].([]any)[0].(map[string]any)
			if got := get(choice, "message", "content"); got != "Hello" {
				t.Errorf("content = %v", got)
			}
			if choice["finish_reason"] != "stop" {
				t.Errorf("finish_reason = %v", choice["finish_reason"])
			}
			if res["model"] != tc.slug {
				t.Errorf("model = %v", res["model"])
			}
			u := res["usage"].(map[string]any)
			if u["prompt_tokens"] != tc.prompt || u["completion_tokens"] != tc.completion {
				t.Errorf("usage = %v", u)
			}
			if !approx(u["cost"].(float64), wantCost(tc.prompt, tc.completion)) {
				t.Errorf("cost = %v, want %v", u["cost"], wantCost(tc.prompt, tc.completion))
			}
			spent += wantCost(tc.prompt, tc.completion)
		})
		t.Run("stream "+tc.slug, func(t *testing.T) {
			res := readStream(t, c.base, key, map[string]any{"model": tc.slug, "messages": msgs, "stream": true})
			if res.text != "Hello" || !res.gotDone || res.finished != "stop" || res.model != tc.slug {
				t.Errorf("stream result = %+v", res)
			}
			if res.usage == nil || res.usage["prompt_tokens"] != tc.prompt || res.usage["completion_tokens"] != tc.completion {
				t.Errorf("stream usage = %v", res.usage)
			}
			spent += wantCost(tc.prompt, tc.completion)
		})
	}

	// "models" fallback list: the primary only has a broken endpoint.
	res := c.must("POST", "/v1/chat/completions", key, map[string]any{"model": "test/broken", "models": []string{"test/openai"}, "messages": msgs})
	if res["model"] != "test/openai" {
		t.Errorf("fallback model = %v", res["model"])
	}
	spent += wantCost(10, 5)

	// All endpoints broken: upstream status is surfaced.
	if status, _ := c.do("POST", "/v1/chat/completions", key, map[string]any{"model": "test/broken", "messages": msgs}); status != http.StatusServiceUnavailable {
		t.Errorf("broken model status = %d", status)
	}
	if status, _ := c.do("POST", "/v1/chat/completions", key, map[string]any{"model": "nope/nope", "messages": msgs}); status != http.StatusNotFound {
		t.Errorf("unknown model status = %d", status)
	}
	if status, _ := c.do("POST", "/v1/chat/completions", "sk-plex-wrong", map[string]any{"model": "test/openai", "messages": msgs}); status != http.StatusUnauthorized {
		t.Errorf("bad key status = %d", status)
	}

	// Embeddings.
	emb := c.must("POST", "/v1/embeddings", key, map[string]any{"model": "test/openai", "input": "hello"})
	if !approx(get(emb, "usage", "cost").(float64), wantCost(8, 0)) {
		t.Errorf("embedding usage = %v", emb["usage"])
	}
	spent += wantCost(8, 0)

	// OpenAI-style model list with per-token pricing, also under /api/v1.
	ml := c.must("GET", "/api/v1/models", "", nil)
	first := ml["data"].([]any)[0].(map[string]any)
	if get(first, "pricing", "prompt") != "0.000002" {
		t.Errorf("pricing = %v", first["pricing"])
	}

	// Billing adds up exactly.
	me = c.must("GET", "/api/me", "", nil)
	if bal := get(me, "user", "balance").(float64); !approx(bal, 10-spent) {
		t.Errorf("balance = %v, want %v", bal, 10-spent)
	}
	keyInfo := c.must("GET", "/v1/key", key, nil)
	if !approx(get(keyInfo, "data", "usage").(float64), spent) {
		t.Errorf("key usage = %v, want %v", keyInfo["data"], spent)
	}
	stats := c.must("GET", "/api/usage/stats?days=7", "", nil)
	if !approx(stats["total_cost"].(float64), spent) || len(stats["daily"].([]any)) != 7 {
		t.Errorf("stats = %v", stats)
	}
	logs := c.must("GET", "/api/usage/logs?page_size=100", "", nil)
	var errorsLogged int
	for _, l := range logs["data"].([]any) {
		if l.(map[string]any)["status"] == "error" {
			errorsLogged++
		}
	}
	if errorsLogged == 0 {
		t.Errorf("failed attempts should be logged")
	}

	// Key credit limit.
	limited := c.must("POST", "/api/keys", "", map[string]any{"name": "limited", "credit_limit": 0})["secret"].(string)
	if status, _ := c.do("POST", "/v1/chat/completions", limited, map[string]any{"model": "test/openai", "messages": msgs}); status != http.StatusPaymentRequired {
		t.Errorf("limited key status = %d", status)
	}

	// Redeem codes.
	codes := c.must("POST", "/api/admin/redeem-codes", "", map[string]any{"amount": 5, "count": 2})["codes"].([]any)
	red := c.must("POST", "/api/credits/redeem", "", map[string]any{"code": strings.ToLower(codes[0].(string))})
	if red["amount"] != 5.0 {
		t.Errorf("redeem = %v", red)
	}
	if status, _ := c.do("POST", "/api/credits/redeem", "", map[string]any{"code": codes[0]}); status != http.StatusBadRequest {
		t.Errorf("double redeem status = %d", status)
	}

	// A second user is not an admin and cannot reach admin routes.
	idp.add("bob", fakeAccount{sub: "2", email: "bob@example.com", name: "Bob", verified: true})
	u2 := env.browser()
	env.signIn(u2, "github", "login", "bob")
	me2 := u2.must("GET", "/api/me", "", nil)
	if get(me2, "user", "role") != "user" {
		t.Errorf("second user role = %v", get(me2, "user", "role"))
	}
	if status, _ := u2.do("GET", "/api/admin/stats", "", nil); status != http.StatusForbidden {
		t.Errorf("non-admin admin access status = %d", status)
	}
	admin := c.must("GET", "/api/admin/stats", "", nil)
	if admin["users"] != 2.0 {
		t.Errorf("admin stats = %v", admin)
	}

	// Cross-site POST to the console API is blocked.
	req, _ := http.NewRequest("POST", c.base+"/api/keys", strings.NewReader(`{}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if resp, err := c.http.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site request not blocked: %v %v", resp.StatusCode, err)
	}

	// SPA fallback.
	status, body := c.do("GET", "/dashboard/anything", "", nil)
	if status != 200 || !strings.Contains(string(body), "console") {
		t.Errorf("spa fallback = %d %s", status, body)
	}
}
