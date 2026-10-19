package provider

import (
	"encoding/json"
	"io"
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
	body := a.body(mustParse(t, toolConversation), "claude", false)
	if body["system"] != "sys" || body["max_tokens"] != 100 {
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
	body := g.body(req)
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
