package server_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAnthropicNative checks that /v1/messages requests arrive unchanged.
func fakeAnthropicNative(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "an-key" {
			http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, 401)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/messages/count_tokens":
			if _, has := body["stream"]; has {
				t.Error("stream sent to count_tokens")
			}
			io.WriteString(w, `{"input_tokens":42}`)
			return
		case "/v1/messages":
		default:
			http.NotFound(w, r)
			return
		}
		if body["model"] == "claude-reject" {
			w.Header().Set("x-should-retry", "false")
			w.WriteHeader(400)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"context_management: Extra inputs are not permitted"}}`)
			return
		}
		if body["model"] != "claude-sonnet-4-5-20250929" {
			t.Errorf("upstream model = %v", body["model"])
		}
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "Claude Code") { // the native /v1/messages requests
			if r.Header.Get("anthropic-beta") != "interleaved-thinking-2025-05-14" || r.Header.Get("anthropic-version") != "2023-06-01" ||
				r.Header.Get("anthropic-future-capability") != "on" || r.Header.Get("x-api-key") != "an-key" {
				t.Errorf("headers not forwarded: %v", r.Header)
			}
			if !strings.Contains(string(raw), `"cache_control":{"type":"ephemeral"}`) || !strings.Contains(string(raw), `"thinking":{"budget_tokens":2048,"type":"enabled"}`) {
				t.Errorf("body not passed through: %s", raw)
			}
		}
		usage := `"usage":{"input_tokens":10,"cache_read_input_tokens":1000,"cache_creation_input_tokens":100,"output_tokens":4}`
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, e := range []string{
				`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5-20250929","content":[],` + usage + `}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hi"}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
				`{"type":"message_stop"}`,
			} {
				var ev struct{ Type string }
				json.Unmarshal([]byte(e), &ev)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, e)
			}
			return
		}
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5-20250929",
			"content":[{"type":"thinking","thinking":"hmm","signature":"sig"},{"type":"text","text":"Hi"}],"stop_reason":"end_turn",`+usage+`}`)
	}))
	t.Cleanup(s.Close)
	return s
}

// fakeOpenAIForMessages checks the Anthropic→OpenAI conversion.
func fakeOpenAIForMessages(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string           `json:"model"`
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		roles := []string{}
		for _, m := range body.Messages {
			roles = append(roles, m["role"].(string))
		}
		if strings.Join(roles, ",") != "system,user,assistant,tool" || len(body.Tools) != 1 {
			t.Errorf("converted request: roles=%v tools=%v", roles, body.Tools)
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, c := range []string{
				`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"role":"assistant","content":"Let me check."},"finish_reason":null}]}`,
				`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
				`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
				`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Rome\"}"}}]},"finish_reason":null}]}`,
				`{"id":"chatcmpl-9","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"chatcmpl-9","choices":[],"usage":{"prompt_tokens":30,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":20}}}`,
			} {
				fmt.Fprintf(w, "data: %s\n\n", c)
			}
			io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		io.WriteString(w, `{"id":"chatcmpl-9","choices":[{"index":0,"message":{"role":"assistant","content":"Sunny.","tool_calls":[{"id":"call_2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Rome\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":8}}`)
	}))
	t.Cleanup(s.Close)
	return s
}

type sse struct {
	name string
	data map[string]any
}

func postMessages(t *testing.T, base, key string, body any, beta string) (int, []byte, []sse) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+"/v1/messages?beta=true", bytes.NewReader(b))
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-future-capability", "on")
	if beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, raw, nil
	}
	var events []sse
	var name string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if n, ok := strings.CutPrefix(line, "event: "); ok {
			name = n
		} else if d, ok := strings.CutPrefix(line, "data: "); ok {
			var m map[string]any
			if err := json.Unmarshal([]byte(d), &m); err != nil {
				t.Fatalf("bad event data %q", d)
			}
			events = append(events, sse{name, m})
		}
	}
	return resp.StatusCode, nil, events
}

func eventTypes(evs []sse) string {
	var out []string
	for _, e := range evs {
		if e.name != e.data["type"] {
			out = append(out, "MISMATCH:"+e.name)
		}
		s := e.name
		if d, ok := e.data["delta"].(map[string]any); ok && d["type"] != nil {
			s += "/" + d["type"].(string)
		}
		if b, ok := e.data["content_block"].(map[string]any); ok {
			s += "/" + b["type"].(string)
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func TestMessagesAPI(t *testing.T) {
	an, oa := fakeAnthropicNative(t), fakeOpenAIForMessages(t)
	c, _, _ := setup(t)
	me := c.must("GET", "/api/me", "", nil)
	c.must("POST", fmt.Sprintf("/api/admin/users/%v/credits", get(me, "user", "id")), "", map[string]any{"amount": 10})
	pAN := get(c.must("POST", "/api/admin/providers", "", map[string]any{"name": "Anthropic", "type": "anthropic", "base_url": an.URL, "api_key": "an-key"}), "provider", "id")
	pOA := get(c.must("POST", "/api/admin/providers", "", map[string]any{"name": "OpenAI", "type": "openai_compatible", "base_url": oa.URL + "/v1"}), "provider", "id")
	// $3 in / $15 out, cache reads $0.30, writes $3.75 per 1M.
	c.must("POST", "/api/admin/models", "", map[string]any{"slug": "anthropic/claude-sonnet-4.5", "input_price": 3, "output_price": 15,
		"cache_read_price": 0.3, "cache_write_price": 3.75,
		"endpoints": []map[string]any{{"provider_id": pAN, "upstream_model": "claude-sonnet-4-5-20250929"}}})
	c.must("POST", "/api/admin/models", "", map[string]any{"slug": "openai/gpt-x", "input_price": 1, "output_price": 2,
		"endpoints": []map[string]any{{"provider_id": pOA, "upstream_model": "gpt-x"}}})
	key := c.must("POST", "/api/keys", "", map[string]any{"name": "cc"})["secret"].(string)

	native := map[string]any{
		"max_tokens": 4096, "thinking": map[string]any{"type": "enabled", "budget_tokens": 2048},
		"system":   []map[string]any{{"type": "text", "text": "You are Claude Code.", "cache_control": map[string]any{"type": "ephemeral"}}},
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	}
	wantCost := (10*3 + 1000*0.3 + 100*3.75 + 4*15) / 1e6

	// Claude Code names models the Anthropic way; aliases resolve them.
	for _, model := range []string{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5", "anthropic/claude-sonnet-4.5"} {
		native["model"] = model
		status, raw, _ := postMessages(t, c.base, key, native, "interleaved-thinking-2025-05-14")
		var msg map[string]any
		json.Unmarshal(raw, &msg)
		if status != 200 || msg["model"] != model || msg["content"].([]any)[1].(map[string]any)["text"] != "Hi" {
			t.Fatalf("native %s = %d %s", model, status, raw)
		}
	}
	// Streaming pass-through keeps every event, including signatures.
	native["model"], native["stream"] = "claude-sonnet-4-5-20250929", true
	status, _, evs := postMessages(t, c.base, key, native, "interleaved-thinking-2025-05-14")
	if status != 200 || eventTypes(evs) != "message_start content_block_start/thinking content_block_delta/thinking_delta "+
		"content_block_delta/signature_delta content_block_stop content_block_start/text content_block_delta/text_delta "+
		"content_block_stop message_delta message_stop" {
		t.Fatalf("native stream = %d %s", status, eventTypes(evs))
	}
	logs := c.must("GET", "/api/admin/logs?page_size=10", "", nil)["data"].([]any)
	l := logs[0].(map[string]any)
	if l["api"] != "messages" || l["prompt_tokens"] != 1110.0 || l["cache_read_tokens"] != 1000.0 || l["cache_write_tokens"] != 100.0 ||
		!approx(l["cost"].(float64), wantCost) || l["stream"] != true {
		t.Errorf("messages log = %v", l)
	}

	// Through an OpenAI-format provider: tool use round trip.
	converted := map[string]any{
		"model": "openai/gpt-x", "max_tokens": 1024,
		"system": "Be helpful.",
		"tools":  []map[string]any{{"name": "get_weather", "description": "w", "input_schema": map[string]any{"type": "object"}}, {"type": "web_search_20250305", "name": "web_search"}},
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{{"type": "text", "text": "Weather in Rome?"}}},
			{"role": "assistant", "content": []map[string]any{{"type": "thinking", "thinking": "x", "signature": ""}, {"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]any{"city": "Rome"}}}},
			{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": "toolu_1", "content": "18°C"}}},
		},
	}
	status, raw, _ := postMessages(t, c.base, key, converted, "")
	var msg map[string]any
	json.Unmarshal(raw, &msg)
	content := msg["content"].([]any)
	if status != 200 || msg["stop_reason"] != "tool_use" || len(content) != 2 ||
		content[1].(map[string]any)["type"] != "tool_use" || fmt.Sprint(content[1].(map[string]any)["input"]) != "map[city:Rome]" ||
		get(msg, "usage", "input_tokens") != 30.0 {
		t.Fatalf("converted = %d %s", status, raw)
	}
	converted["stream"] = true
	status, _, evs = postMessages(t, c.base, key, converted, "")
	if got := eventTypes(evs); status != 200 || got != "message_start content_block_start/text content_block_delta/text_delta "+
		"content_block_stop content_block_start/tool_use content_block_delta/input_json_delta content_block_delta/input_json_delta "+
		"content_block_stop message_delta message_stop" {
		t.Fatalf("converted stream = %d %s", status, got)
	}
	last := evs[len(evs)-2].data
	if get(last, "delta", "stop_reason") != "tool_use" || get(last, "usage", "input_tokens") != 10.0 || get(last, "usage", "cache_read_input_tokens") != 20.0 {
		t.Errorf("message_delta = %v", last)
	}

	// count_tokens: exact via Anthropic, estimated otherwise.
	countReq := func(model string) map[string]any {
		b, _ := json.Marshal(map[string]any{"model": model, "messages": []map[string]any{{"role": "user", "content": "hello there"}}})
		req, _ := http.NewRequest("POST", c.base+"/v1/messages/count_tokens", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	if n := countReq("claude-sonnet-4-5")["input_tokens"]; n != 42.0 {
		t.Errorf("anthropic count_tokens = %v", n)
	}
	if n, _ := countReq("openai/gpt-x")["input_tokens"].(float64); n <= 0 {
		t.Errorf("estimated count_tokens = %v", n)
	}

	// Upstream rejections of the request reach the client unmodified.
	c.must("POST", "/api/admin/models", "", map[string]any{"slug": "anthropic/claude-reject", "input_price": 1, "output_price": 1,
		"endpoints": []map[string]any{{"provider_id": pAN, "upstream_model": "claude-reject"}}})
	native["model"], native["stream"] = "anthropic/claude-reject", false
	status, raw, _ = postMessages(t, c.base, key, native, "")
	if status != 400 || string(raw) != `{"type":"error","error":{"type":"invalid_request_error","message":"context_management: Extra inputs are not permitted"}}` {
		t.Errorf("forwarded rejection = %d %s", status, raw)
	}
	native["model"] = "claude-sonnet-4-5"

	// Errors use the Anthropic shape.
	status, raw, _ = postMessages(t, c.base, "sk-plex-wrong", native, "")
	if status != 401 || !strings.Contains(string(raw), `"type":"authentication_error"`) {
		t.Errorf("auth error = %d %s", status, raw)
	}
	native["model"] = "claude-unknown"
	status, raw, _ = postMessages(t, c.base, key, native, "")
	if status != 404 || !strings.Contains(string(raw), `"type":"not_found_error"`) {
		t.Errorf("unknown model = %d %s", status, raw)
	}

	// Chat completions benefit from aliases too, and report cache usage.
	cs, cb := c.do("POST", "/v1/chat/completions", key, map[string]any{"model": "claude-sonnet-4-5",
		"messages": []map[string]any{{"role": "system", "content": "be brief"}, {"role": "user", "content": "hi"}}})
	if cs != 200 || !strings.Contains(string(cb), `"cached_tokens":1000`) || !strings.Contains(string(cb), `"model":"anthropic/claude-sonnet-4.5"`) {
		t.Errorf("chat via alias = %d %s", cs, cb)
	}
}
