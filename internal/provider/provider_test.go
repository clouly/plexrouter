package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustParse(t *testing.T, body string) *ChatRequest {
	t.Helper()
	req, err := ParseChatRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const toolConversation = `{
	"model": "x",
	"messages": [
		{"role": "system", "content": "sys"},
		{"role": "user", "content": [{"type": "text", "text": "weather?"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}]},
		{"role": "assistant", "content": null, "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}}]},
		{"role": "tool", "tool_call_id": "call_1", "content": "{\"temp\":20}"},
		{"role": "user", "content": "thanks"}
	],
	"tools": [{"type": "function", "function": {"name": "get_weather", "parameters": {"type": "object"}}}],
	"tool_choice": "required",
	"stop": "END",
	"max_tokens": 100
}`

func TestAnthropicBody(t *testing.T) {
	a := &anthropic{}
	body, _ := a.body(mustParse(t, toolConversation), "claude", false)
	if toJSON(t, body["system"]) != `[{"text":"sys","type":"text"}]` || body["max_tokens"] != 100 {
		t.Fatalf("body = %v", body)
	}
	msgs := body["messages"].([]any)
	// user, assistant(tool_use), user(tool_result + text merged)
	if len(msgs) != 3 {
		t.Fatalf("messages = %s", toJSON(t, msgs))
	}
	got := toJSON(t, msgs)
	for _, want := range []string{`{"source":{"data":"AAAA","media_type":"image/png","type":"base64"},"type":"image"}`,
		`"input":{"city":"Paris"}`, `"tool_use_id":"call_1","type":"tool_result"`, `"text":"thanks"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if toJSON(t, body["tool_choice"]) != `{"type":"any"}` || toJSON(t, body["stop_sequences"]) != `["END"]` {
		t.Errorf("tool_choice/stop = %v %v", body["tool_choice"], body["stop_sequences"])
	}
}

func TestGeminiBodyAndSignatureRoundTrip(t *testing.T) {
	g := &gemini{}
	id := newToolCallID("SIG+/=")
	req := mustParse(t, strings.Replace(toolConversation, `"call_1"`, `"`+id+`"`, -1))
	body := g.body(req, "gemini-2.5-pro")
	got := toJSON(t, body["contents"])
	for _, want := range []string{`"inlineData":{"data":"AAAA","mimeType":"image/png"}`,
		`"functionCall":{"args":{"city":"Paris"},"name":"get_weather"},"thoughtSignature":"SIG+/="`,
		`"functionResponse":{"name":"get_weather","response":{"temp":20}}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if toJSON(t, body["toolConfig"]) != `{"functionCallingConfig":{"mode":"ANY"}}` {
		t.Errorf("toolConfig = %v", body["toolConfig"])
	}
}

func TestGeminiToolCallResponse(t *testing.T) {
	var r geminiResponse
	json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","args":{"a":1}},"thoughtSignature":"S"}]},"finishReason":"STOP"}]}`), &r)
	msg, finish, n := r.delta(0, false)
	if n != 1 || finish != "stop" {
		t.Fatalf("n=%d finish=%v", n, finish)
	}
	call := msg["tool_calls"].([]any)[0].(Object)
	if !strings.HasSuffix(call["id"].(string), thoughtSigSep+"S") || toJSON(t, call["function"]) != `{"arguments":"{\"a\":1}","name":"f"}` {
		t.Errorf("call = %v", call)
	}
}

func TestOpenAIBodyStripsGatewayFields(t *testing.T) {
	o := &openAI{}
	body := o.body(mustParse(t, `{"model":"a/b","models":["c"],"provider":{},"messages":[],"stream":true,"seed":7}`), "b", true)
	if _, ok := body["models"]; ok {
		t.Error("models leaked")
	}
	if _, ok := body["provider"]; ok {
		t.Error("provider leaked")
	}
	if body["model"] != "b" || toJSON(t, body["seed"]) != "7" || toJSON(t, body["stream_options"]) != `{"include_usage":true}` {
		t.Errorf("body = %v", body)
	}
}

func TestSSEReader(t *testing.T) {
	in := ": comment\nevent: x\ndata: {\"a\":1}\n\ndata: line1\ndata: line2\r\n\r\ndata: last"
	r := newSSEReader(io.NopCloser(strings.NewReader(in)))
	var got []string
	for {
		d, err := r.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(d))
	}
	if strings.Join(got, "|") != `{"a":1}|line1`+"\n"+`line2|last` {
		t.Errorf("got %q", got)
	}
}

func TestRetryable(t *testing.T) {
	cases := map[int]bool{400: false, 401: true, 404: true, 422: false, 429: true, 500: true, 503: true, 0: true}
	for status, want := range cases {
		if got := Retryable(&Error{Status: status}); got != want {
			t.Errorf("Retryable(%d) = %v", status, got)
		}
	}
}

func TestAnthropicThinking(t *testing.T) {
	a := &anthropic{}
	body, _ := a.body(mustParse(t, `{"model":"x","reasoning_effort":"high","temperature":0.2,"messages":[{"role":"user","content":"hi"}]}`), "claude", false)
	if toJSON(t, body["thinking"]) != `{"budget_tokens":16384,"type":"enabled"}` || body["max_tokens"] != 16384+anthropicDefaultMaxTokens {
		t.Errorf("thinking = %v, max_tokens = %v", body["thinking"], body["max_tokens"])
	}
	if _, ok := body["temperature"]; ok {
		t.Error("temperature must be dropped with thinking")
	}
	// An explicit budget that doesn't fit max_tokens is clamped.
	body, _ = a.body(mustParse(t, `{"model":"x","max_tokens":3000,"reasoning":{"max_tokens":5000},"messages":[{"role":"user","content":"hi"}]}`), "claude", false)
	if toJSON(t, body["thinking"]) != `{"budget_tokens":2999,"type":"enabled"}` || body["max_tokens"] != 3000 {
		t.Errorf("clamped thinking = %v", body["thinking"])
	}
	// Tool-use history: thinking is skipped (prior thinking blocks are unavailable).
	body, _ = a.body(mustParse(t, strings.Replace(toolConversation, `"max_tokens": 100`, `"max_tokens": 100, "reasoning_effort": "low"`, 1)), "claude", false)
	if _, ok := body["thinking"]; ok {
		t.Error("thinking enabled despite tool history")
	}
	// Disabled explicitly.
	body, _ = a.body(mustParse(t, `{"model":"x","reasoning":{"enabled":false,"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`), "claude", false)
	if _, ok := body["thinking"]; ok {
		t.Error("thinking enabled despite enabled:false")
	}
}

func TestAnthropicCacheControl(t *testing.T) {
	a := &anthropic{}
	body, _ := a.body(mustParse(t, `{"model":"x","messages":[
		{"role":"system","content":[{"type":"text","text":"long context","cache_control":{"type":"ephemeral"}}]},
		{"role":"user","content":[{"type":"text","text":"q","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`), "claude", false)
	if got := toJSON(t, body["system"]); got != `[{"cache_control":{"type":"ephemeral"},"text":"long context","type":"text"}]` {
		t.Errorf("system = %s", got)
	}
	if got := toJSON(t, body["messages"]); !strings.Contains(got, `"cache_control":{"type":"ephemeral","ttl":"1h"}`) {
		t.Errorf("messages = %s", got)
	}
}

func TestAnthropicJSONSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if toJSON(t, body["tool_choice"]) != `{"name":"json_response","type":"tool"}` {
			t.Errorf("tool_choice = %v", body["tool_choice"])
		}
		io.WriteString(w, `{"id":"m","content":[{"type":"tool_use","id":"t","name":"json_response","input":{"city":"Paris"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer srv.Close()
	a := &anthropic{httpClient{client: srv.Client(), base: srv.URL, key: "k"}}
	resp, err := a.Chat(context.Background(), mustParse(t, `{"model":"x","messages":[{"role":"user","content":"where?"}],
		"response_format":{"type":"json_schema","json_schema":{"name":"loc","schema":{"type":"object","properties":{"city":{"type":"string"}}}}}}`), "claude")
	if err != nil {
		t.Fatal(err)
	}
	choice := resp["choices"].([]any)[0].(Object)
	msg := choice["message"].(Object)
	if msg["content"] != `{"city":"Paris"}` || choice["finish_reason"] != "stop" || msg["tool_calls"] != nil {
		t.Errorf("json mode response = %v", choice)
	}
}

func TestAnthropicCacheUsage(t *testing.T) {
	u := anthropicUsage{InputTokens: 10, OutputTokens: 4, CacheCreationInputTokens: 100, CacheReadInputTokens: 1000}.usage()
	if u.PromptTokens != 1110 || u.CacheReadTokens != 1000 || u.CacheWriteTokens != 100 {
		t.Errorf("usage = %+v", u)
	}
	obj := Object{"usage": usageObject(u)}
	if got := UsageOf(decode(t, toJSON(t, obj))); *got != u {
		t.Errorf("round trip = %+v", got)
	}
}

func decode(t *testing.T, s string) Object {
	t.Helper()
	obj, err := decodeObject(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestGeminiThinking(t *testing.T) {
	g := &gemini{}
	req := mustParse(t, `{"model":"x","reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]}`)
	cfg := toJSON(t, g.body(req, "gemini-2.5-flash")["generationConfig"])
	if cfg != `{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":2048}}` {
		t.Errorf("2.5 config = %s", cfg)
	}
	cfg = toJSON(t, g.body(req, "gemini-3-pro-preview")["generationConfig"])
	if cfg != `{"thinkingConfig":{"includeThoughts":true,"thinkingLevel":"low"}}` {
		t.Errorf("3 config = %s", cfg)
	}
}

func TestOpenAIOfficialVsCompatible(t *testing.T) {
	req := mustParse(t, `{"model":"x","reasoning":{"effort":"high"},"messages":[{"role":"user","content":[{"type":"text","text":"q","cache_control":{"type":"ephemeral"}}]}]}`)
	official := (&openAI{official: true}).body(req, "o3", false)
	if official["reasoning_effort"] != "high" || official["reasoning"] != nil || strings.Contains(toJSON(t, official["messages"]), "cache_control") {
		t.Errorf("official body = %s", toJSON(t, official))
	}
	// The original request must not be mutated by stripping.
	if !strings.Contains(toJSON(t, req.Raw["messages"]), "cache_control") {
		t.Error("stripCacheControl mutated the request")
	}
	compat := (&openAI{}).body(req, "x", false)
	if compat["reasoning"] == nil || !strings.Contains(toJSON(t, compat["messages"]), "cache_control") {
		t.Errorf("compatible body = %s", toJSON(t, compat))
	}
}

func TestSpliceModel(t *testing.T) {
	in := "{\n  \"system\": [{\"type\":\"text\",\"text\":\"x\"}],\n  \"model\" :  \"claude-sonnet-4-5\" , \"z\":1, \"nested\":{\"model\":\"keep\"}}"
	out, err := SpliceModel([]byte(in), "claude-sonnet-4-5-20250929")
	want := strings.Replace(in, `"claude-sonnet-4-5"`, `"claude-sonnet-4-5-20250929"`, 1)
	if err != nil || string(out) != want {
		t.Errorf("SpliceModel = %q, %v", out, err)
	}
	if _, err := SpliceModel([]byte(`{"messages":[]}`), "m"); err == nil {
		t.Error("missing model should fail")
	}
}
