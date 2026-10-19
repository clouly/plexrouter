package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// gemini converts to and from the Google Gemini (Generative Language) API.
type gemini struct{ httpClient }

func (g *gemini) url(model, method string) string {
	base := g.base
	if !strings.HasSuffix(base, "/v1beta") && !strings.HasSuffix(base, "/v1") {
		base += "/v1beta"
	}
	return base + "/models/" + strings.TrimPrefix(model, "models/") + ":" + method
}

func (g *gemini) header() http.Header {
	h := http.Header{}
	h.Set("x-goog-api-key", g.key)
	return h
}

// Gemini 3 requires the thought signature of a function call to be sent back
// with it. OpenAI clients only echo the tool call ID, so the signature rides
// along inside it.
const thoughtSigSep = "__ts_"

func newToolCallID(signature string) string {
	b := make([]byte, 12)
	rand.Read(b)
	id := "call_" + hex.EncodeToString(b)
	if signature != "" {
		id += thoughtSigSep + signature
	}
	return id
}

func (g *gemini) body(req *ChatRequest) Object {
	var system []string
	toolNames := map[string]string{} // tool call id -> function name
	type turn struct {
		role  string
		parts []any
	}
	var turns []turn
	push := func(role string, parts []any) {
		if len(parts) == 0 {
			return
		}
		if n := len(turns); n > 0 && turns[n-1].role == role {
			turns[n-1].parts = append(turns[n-1].parts, parts...)
			return
		}
		turns = append(turns, turn{role, parts})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if t := m.Text(); t != "" {
				system = append(system, t)
			}
		case "assistant":
			var parts []any
			if t := m.Text(); t != "" {
				parts = append(parts, Object{"text": t})
			}
			for _, tc := range m.ToolCalls {
				toolNames[tc.ID] = tc.Function.Name
				var args any = Object{}
				if tc.Function.Arguments != "" {
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}
				part := Object{"functionCall": Object{"name": tc.Function.Name, "args": args}}
				if _, sig, ok := strings.Cut(tc.ID, thoughtSigSep); ok {
					part["thoughtSignature"] = sig
				}
				parts = append(parts, part)
			}
			push("model", parts)
		case "tool":
			text := m.Text()
			var response any
			if json.Unmarshal([]byte(text), &response) != nil {
				response = nil
			}
			if _, isObj := response.(map[string]any); !isObj {
				response = Object{"content": text}
			}
			name := toolNames[m.ToolCallID]
			if name == "" {
				name = m.Name
			}
			push("user", []any{Object{"functionResponse": Object{"name": name, "response": response}}})
		default:
			push("user", geminiParts(m.Parts()))
		}
	}

	contents := make([]any, 0, len(turns))
	for _, t := range turns {
		contents = append(contents, Object{"role": t.role, "parts": t.parts})
	}
	body := Object{"contents": contents}
	if len(system) > 0 {
		body["systemInstruction"] = Object{"parts": []any{Object{"text": strings.Join(system, "\n\n")}}}
	}

	cfg := Object{}
	if req.Temperature != nil {
		cfg["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		cfg["topP"] = *req.TopP
	}
	if req.TopK != nil {
		cfg["topK"] = *req.TopK
	}
	if n := req.MaxOutputTokens(); n > 0 {
		cfg["maxOutputTokens"] = n
	}
	if stop := req.StopSequences(); len(stop) > 0 {
		cfg["stopSequences"] = stop
	}
	if rf := req.ResponseFormat; rf != nil {
		switch rf.Type {
		case "json_object":
			cfg["responseMimeType"] = "application/json"
		case "json_schema":
			cfg["responseMimeType"] = "application/json"
			if rf.JSONSchema != nil && len(rf.JSONSchema.Schema) > 0 {
				cfg["responseJsonSchema"] = rf.JSONSchema.Schema
			}
		}
	}
	if len(cfg) > 0 {
		body["generationConfig"] = cfg
	}

	if len(req.Tools) > 0 {
		decls := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			d := Object{"name": t.Function.Name, "description": t.Function.Description}
			if len(t.Function.Parameters) > 0 && string(t.Function.Parameters) != "null" {
				d["parametersJsonSchema"] = t.Function.Parameters
			}
			decls = append(decls, d)
		}
		body["tools"] = []any{Object{"functionDeclarations": decls}}
		if tc := geminiToolConfig(req.ToolChoice); tc != nil {
			body["toolConfig"] = tc
		}
	}
	return body
}

func geminiParts(parts []ContentPart) []any {
	var out []any
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				out = append(out, Object{"text": p.Text})
			}
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			if mt, data, ok := parseDataURL(p.ImageURL.URL); ok {
				out = append(out, Object{"inlineData": Object{"mimeType": mt, "data": data}})
			} else {
				mt := mime.TypeByExtension(path.Ext(strings.SplitN(p.ImageURL.URL, "?", 2)[0]))
				if mt == "" {
					mt = "image/jpeg"
				}
				out = append(out, Object{"fileData": Object{"mimeType": mt, "fileUri": p.ImageURL.URL}})
			}
		}
	}
	return out
}

func geminiToolConfig(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		mode := map[string]string{"auto": "AUTO", "none": "NONE", "required": "ANY"}[s]
		if mode == "" {
			return nil
		}
		return Object{"functionCallingConfig": Object{"mode": mode}}
	}
	var obj struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Function.Name != "" {
		return Object{"functionCallingConfig": Object{"mode": "ANY", "allowedFunctionNames": []string{obj.Function.Name}}}
	}
	return nil
}

type geminiResponse struct {
	ResponseID string `json:"responseId"`
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text             string `json:"text"`
				Thought          bool   `json:"thought"`
				ThoughtSignature string `json:"thoughtSignature"`
				FunctionCall     *struct {
					Name string          `json:"name"`
					Args json.RawMessage `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
}

func (r *geminiResponse) usage() (prompt, completion int, ok bool) {
	if r.UsageMetadata == nil {
		return 0, 0, false
	}
	u := r.UsageMetadata
	return u.PromptTokenCount, u.CandidatesTokenCount + u.ThoughtsTokenCount, true
}

// delta converts the first candidate into OpenAI message fields. toolIndex is
// the number of tool calls emitted so far (for streaming indices).
func (r *geminiResponse) delta(toolIndex int, streaming bool) (msg Object, finish any, toolCalls int) {
	msg = Object{}
	if len(r.Candidates) == 0 {
		if r.PromptFeedback != nil && r.PromptFeedback.BlockReason != "" {
			return msg, "content_filter", 0
		}
		return msg, nil, 0
	}
	c := r.Candidates[0]
	var text, reasoning strings.Builder
	var calls []any
	for _, p := range c.Content.Parts {
		switch {
		case p.FunctionCall != nil:
			args := string(p.FunctionCall.Args)
			if args == "" || args == "null" {
				args = "{}"
			}
			call := Object{"id": newToolCallID(p.ThoughtSignature), "type": "function",
				"function": Object{"name": p.FunctionCall.Name, "arguments": args}}
			if streaming {
				call["index"] = toolIndex + len(calls)
			}
			calls = append(calls, call)
		case p.Thought:
			reasoning.WriteString(p.Text)
		default:
			text.WriteString(p.Text)
		}
	}
	if text.Len() > 0 || !streaming {
		msg["content"] = text.String()
	}
	if reasoning.Len() > 0 {
		msg["reasoning"] = reasoning.String()
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	switch c.FinishReason {
	case "":
	case "STOP":
		finish = "stop"
	case "MAX_TOKENS":
		finish = "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		finish = "content_filter"
	default:
		finish = "stop"
	}
	return msg, finish, len(calls)
}

func (g *gemini) Chat(ctx context.Context, req *ChatRequest, upstreamModel string) (Object, error) {
	resp, err := g.do(ctx, http.MethodPost, g.url(upstreamModel, "generateContent"), g.body(req), g.header())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Message: "invalid JSON from upstream: " + err.Error()}
	}
	msg, finish, calls := r.delta(0, false)
	msg["role"] = "assistant"
	if calls > 0 && finish == "stop" {
		finish = "tool_calls"
	}
	prompt, completionTokens, _ := r.usage()
	return completion(r.ResponseID, upstreamModel, unixNow(), msg, finish, usageObject(prompt, completionTokens)), nil
}

func (g *gemini) ChatStream(ctx context.Context, req *ChatRequest, upstreamModel string) (Stream, error) {
	resp, err := g.do(ctx, http.MethodPost, g.url(upstreamModel, "streamGenerateContent")+"?alt=sse", g.body(req), g.header())
	if err != nil {
		return nil, err
	}
	return &geminiStream{sse: newSSEReader(resp.Body), model: upstreamModel, created: unixNow(), id: newToolCallID("")[5:]}, nil
}

type geminiStream struct {
	sse                *sseReader
	model, id          string
	created            int64
	started            bool
	toolCalls          int
	prompt, completion int
	sawUsage, done     bool
}

func (s *geminiStream) Next() (Object, error) {
	for {
		if s.done {
			return nil, io.EOF
		}
		data, err := s.sse.next()
		if err == io.EOF {
			s.done = true
			c := chunk(s.id, s.model, s.created, Object{}, nil)
			c["choices"] = []any{}
			if s.sawUsage {
				c["usage"] = usageObject(s.prompt, s.completion)
			}
			return c, nil
		}
		if err != nil {
			return nil, err
		}
		var r geminiResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, &Error{Status: http.StatusBadGateway, Message: "invalid stream event: " + err.Error()}
		}
		if p, c, ok := r.usage(); ok {
			s.prompt, s.completion, s.sawUsage = p, c, true
		}
		delta, finish, calls := r.delta(s.toolCalls, true)
		s.toolCalls += calls
		if finish == "stop" && s.toolCalls > 0 {
			finish = "tool_calls"
		}
		if !s.started {
			s.started = true
			delta["role"] = "assistant"
		}
		if len(delta) == 0 && finish == nil {
			continue
		}
		return chunk(s.id, s.model, s.created, delta, finish), nil
	}
}

func (s *geminiStream) Close() error { return s.sse.Close() }

func (g *gemini) Embeddings(context.Context, Object, string) (Object, error) {
	return nil, ErrUnsupported
}

func (g *gemini) ListModels(ctx context.Context) ([]string, error) {
	base := g.base
	if !strings.HasSuffix(base, "/v1beta") && !strings.HasSuffix(base, "/v1") {
		base += "/v1beta"
	}
	var ids []string
	token := ""
	for page := 0; page < 10; page++ {
		u := base + "/models?pageSize=1000"
		if token != "" {
			u += "&pageToken=" + url.QueryEscape(token)
		}
		resp, err := g.do(ctx, http.MethodGet, u, nil, g.header())
		if err != nil {
			return nil, err
		}
		var out struct {
			Models []struct {
				Name    string   `json:"name"`
				Methods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil {
			return nil, &Error{Status: http.StatusBadGateway, Message: err.Error()}
		}
		for _, m := range out.Models {
			for _, method := range m.Methods {
				if method == "generateContent" {
					ids = append(ids, strings.TrimPrefix(m.Name, "models/"))
					break
				}
			}
		}
		if token = out.NextPageToken; token == "" {
			break
		}
	}
	return ids, nil
}
