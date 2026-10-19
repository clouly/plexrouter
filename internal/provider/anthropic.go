package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropic converts to and from the Anthropic Messages API.
type anthropic struct{ httpClient }

const anthropicDefaultMaxTokens = 4096

func (a *anthropic) url(path string) string {
	if strings.HasSuffix(a.base, "/v1") {
		return a.base + path
	}
	return a.base + "/v1" + path
}

func (a *anthropic) header() http.Header {
	h := http.Header{}
	h.Set("x-api-key", a.key)
	h.Set("anthropic-version", "2023-06-01")
	return h
}

// jsonToolName is the forced tool used to get JSON-schema output from Claude.
const jsonToolName = "json_response"

// body builds a Messages API request. jsonMode reports that the answer will
// arrive as the input of the forced jsonToolName tool call.
func (a *anthropic) body(req *ChatRequest, upstreamModel string, stream bool) (body Object, jsonMode bool) {
	var system []any // text blocks; cache_control is kept per block
	type turn struct {
		role   string
		blocks []any
	}
	var turns []turn
	push := func(role string, blocks []any) {
		if len(blocks) == 0 {
			return
		}
		// Anthropic requires alternating roles; merge consecutive turns.
		if n := len(turns); n > 0 && turns[n-1].role == role {
			turns[n-1].blocks = append(turns[n-1].blocks, blocks...)
			return
		}
		turns = append(turns, turn{role, blocks})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			for _, p := range m.Parts() {
				if p.Type == "text" && p.Text != "" {
					system = append(system, textBlock(p))
				}
			}
		case "assistant":
			var blocks []any
			if t := m.Text(); t != "" {
				blocks = append(blocks, Object{"type": "text", "text": t})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Function.Arguments)
				if !json.Valid(input) || len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, Object{"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": input})
			}
			push("assistant", blocks)
		case "tool":
			push("user", []any{Object{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Text()}})
		default: // user
			push("user", anthropicContent(m.Parts()))
		}
	}

	messages := make([]any, 0, len(turns))
	for _, t := range turns {
		messages = append(messages, Object{"role": t.role, "content": t.blocks})
	}
	maxTokens := req.MaxOutputTokens()
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	body = Object{"model": upstreamModel, "messages": messages, "max_tokens": maxTokens}
	if len(system) > 0 {
		body["system"] = system
	}
	if stream {
		body["stream"] = true
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if req.TopK != nil {
		body["top_k"] = *req.TopK
	}
	if stop := req.StopSequences(); len(stop) > 0 {
		body["stop_sequences"] = stop
	}
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if len(schema) == 0 || string(schema) == "null" {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, Object{"name": t.Function.Name, "description": t.Function.Description, "input_schema": schema})
		}
		body["tools"] = tools
		if tc := anthropicToolChoice(req.ToolChoice); tc != nil {
			body["tool_choice"] = tc
		}
	}
	if req.User != "" {
		body["metadata"] = Object{"user_id": req.User}
	}

	// Structured output: Claude reliably follows a schema when it must "call"
	// a tool with it; the tool input is returned as the message content.
	if rf := req.ResponseFormat; rf != nil && len(req.Tools) == 0 {
		switch rf.Type {
		case "json_schema":
			schema := json.RawMessage(`{"type":"object"}`)
			if rf.JSONSchema != nil && len(rf.JSONSchema.Schema) > 0 {
				schema = rf.JSONSchema.Schema
			}
			body["tools"] = []any{Object{"name": jsonToolName, "description": "Return the answer as JSON matching this schema.", "input_schema": schema}}
			body["tool_choice"] = Object{"type": "tool", "name": jsonToolName}
			jsonMode = true
		case "json_object":
			system = append(system, Object{"type": "text", "text": "Respond only with a single valid JSON object, without any other text."})
			body["system"] = system
		}
	}

	// Extended thinking. Anthropic requires earlier thinking blocks to be sent
	// back in tool-use conversations, which OpenAI-format clients don't keep,
	// and forbids it with a forced tool; skip it in those cases.
	_, forced := body["tool_choice"].(Object)
	if tc, ok := body["tool_choice"].(Object); ok && (tc["type"] == "auto" || tc["type"] == "none") {
		forced = false
	}
	if effort, explicit := req.ReasoningRequest(); effort != "" && !forced && !req.HasToolHistory() {
		budget := thinkingBudget(effort, explicit)
		if req.MaxOutputTokens() <= 0 {
			body["max_tokens"] = budget + anthropicDefaultMaxTokens
		} else if budget >= maxTokens {
			budget = maxTokens - 1
		}
		if budget >= 1024 {
			body["thinking"] = Object{"type": "enabled", "budget_tokens": budget}
			// Sampling parameters can't be combined with thinking.
			delete(body, "temperature")
			delete(body, "top_p")
			delete(body, "top_k")
		}
	}
	return body, jsonMode
}

func textBlock(p ContentPart) Object {
	b := Object{"type": "text", "text": p.Text}
	if len(p.CacheControl) > 0 && string(p.CacheControl) != "null" {
		b["cache_control"] = p.CacheControl
	}
	return b
}

func anthropicContent(parts []ContentPart) []any {
	var blocks []any
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				blocks = append(blocks, textBlock(p))
			}
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			var img Object
			if mime, data, ok := parseDataURL(p.ImageURL.URL); ok {
				img = Object{"type": "image", "source": Object{"type": "base64", "media_type": mime, "data": data}}
			} else {
				img = Object{"type": "image", "source": Object{"type": "url", "url": p.ImageURL.URL}}
			}
			if len(p.CacheControl) > 0 && string(p.CacheControl) != "null" {
				img["cache_control"] = p.CacheControl
			}
			blocks = append(blocks, img)
		}
	}
	return blocks
}

func anthropicToolChoice(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch s {
		case "auto":
			return Object{"type": "auto"}
		case "none":
			return Object{"type": "none"}
		case "required":
			return Object{"type": "any"}
		}
		return nil
	}
	var obj struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Function.Name != "" {
		return Object{"type": "tool", "name": obj.Function.Name}
	}
	return nil
}

// parseDataURL splits "data:image/png;base64,AAAA" into mime type and payload.
func parseDataURL(u string) (mime, data string, ok bool) {
	rest, found := strings.CutPrefix(u, "data:")
	if !found {
		return "", "", false
	}
	meta, data, found := strings.Cut(rest, ",")
	if !found || !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	return strings.TrimSuffix(meta, ";base64"), data, true
}

func anthropicFinish(reason string) any {
	switch reason {
	case "":
		return nil
	case "end_turn", "stop_sequence", "pause_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	}
	return "stop"
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u anthropicUsage) usage() Usage {
	return Usage{PromptTokens: u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		CompletionTokens: u.OutputTokens, CacheReadTokens: u.CacheReadInputTokens, CacheWriteTokens: u.CacheCreationInputTokens}
}

type anthropicBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

func (a *anthropic) Chat(ctx context.Context, req *ChatRequest, upstreamModel string) (Object, error) {
	body, jsonMode := a.body(req, upstreamModel, false)
	resp, err := a.do(ctx, http.MethodPost, a.url("/messages"), body, a.header())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var msg struct {
		ID         string           `json:"id"`
		Content    []anthropicBlock `json:"content"`
		StopReason string           `json:"stop_reason"`
		Usage      anthropicUsage   `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Message: "invalid JSON from upstream: " + err.Error()}
	}
	var text, reasoning strings.Builder
	var toolCalls []any
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "thinking":
			reasoning.WriteString(b.Thinking)
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			if jsonMode && b.Name == jsonToolName {
				text.WriteString(args)
				continue
			}
			toolCalls = append(toolCalls, Object{"id": b.ID, "type": "function", "function": Object{"name": b.Name, "arguments": args}})
		}
	}
	message := Object{"role": "assistant", "content": text.String()}
	if reasoning.Len() > 0 {
		message["reasoning"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	finish := anthropicFinish(msg.StopReason)
	if jsonMode && finish == "tool_calls" {
		finish = "stop"
	}
	return completion(msg.ID, upstreamModel, unixNow(), message, finish, usageObject(msg.Usage.usage())), nil
}

func (a *anthropic) ChatStream(ctx context.Context, req *ChatRequest, upstreamModel string) (Stream, error) {
	body, jsonMode := a.body(req, upstreamModel, true)
	resp, err := a.do(ctx, http.MethodPost, a.url("/messages"), body, a.header())
	if err != nil {
		return nil, err
	}
	return &anthropicStream{sse: newSSEReader(resp.Body), model: upstreamModel, created: unixNow(), toolIndex: map[int]int{},
		jsonMode: jsonMode, jsonBlock: -1}, nil
}

type anthropicStream struct {
	sse       *sseReader
	model     string
	id        string
	created   int64
	usage     anthropicUsage
	toolIndex map[int]int // content block index -> tool_calls index
	jsonMode  bool        // answer arrives as the forced JSON tool's input
	jsonBlock int         // content block index of that tool call, -1 if none
	finished  bool
	done      bool
}

func (s *anthropicStream) Next() (Object, error) {
	for {
		if s.done {
			return nil, io.EOF
		}
		data, err := s.sse.next()
		if err == io.EOF && !s.finished {
			return nil, &Error{Status: http.StatusBadGateway, Message: "stream ended before message_stop"}
		}
		if err != nil {
			return nil, err
		}
		var ev struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				ID    string         `json:"id"`
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
			ContentBlock anthropicBlock `json:"content_block"`
			Delta        struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage *anthropicUsage `json:"usage"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, &Error{Status: http.StatusBadGateway, Message: "invalid stream event: " + err.Error()}
		}
		switch ev.Type {
		case "message_start":
			s.id = ev.Message.ID
			s.usage = ev.Message.Usage
			return chunk(s.id, s.model, s.created, Object{"role": "assistant", "content": ""}, nil), nil
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" && s.jsonMode && ev.ContentBlock.Name == jsonToolName {
				s.jsonBlock = ev.Index
				continue
			}
			if ev.ContentBlock.Type == "tool_use" {
				i := len(s.toolIndex)
				s.toolIndex[ev.Index] = i
				return chunk(s.id, s.model, s.created, Object{"tool_calls": []any{Object{
					"index": i, "id": ev.ContentBlock.ID, "type": "function",
					"function": Object{"name": ev.ContentBlock.Name, "arguments": ""},
				}}}, nil), nil
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				return chunk(s.id, s.model, s.created, Object{"content": ev.Delta.Text}, nil), nil
			case "thinking_delta":
				return chunk(s.id, s.model, s.created, Object{"reasoning": ev.Delta.Thinking}, nil), nil
			case "input_json_delta":
				if ev.Index == s.jsonBlock {
					return chunk(s.id, s.model, s.created, Object{"content": ev.Delta.PartialJSON}, nil), nil
				}
				return chunk(s.id, s.model, s.created, Object{"tool_calls": []any{Object{
					"index": s.toolIndex[ev.Index], "function": Object{"arguments": ev.Delta.PartialJSON},
				}}}, nil), nil
			}
		case "message_delta":
			if ev.Usage != nil {
				s.usage.OutputTokens = ev.Usage.OutputTokens
				if ev.Usage.InputTokens > 0 {
					s.usage.InputTokens = ev.Usage.InputTokens
				}
			}
			if ev.Delta.StopReason != "" {
				finish := anthropicFinish(ev.Delta.StopReason)
				if s.jsonMode && finish == "tool_calls" {
					finish = "stop"
				}
				return chunk(s.id, s.model, s.created, Object{}, finish), nil
			}
		case "message_stop":
			s.finished, s.done = true, true
			c := chunk(s.id, s.model, s.created, Object{}, nil)
			c["choices"] = []any{}
			c["usage"] = usageObject(s.usage.usage())
			return c, nil
		case "error":
			return nil, &Error{Status: http.StatusBadGateway, Message: errorMessage(data)}
		}
		// ping, content_block_stop, signature deltas: nothing to emit.
	}
}

func (s *anthropicStream) Close() error { return s.sse.Close() }

func (a *anthropic) Embeddings(context.Context, Object, string) (Object, error) {
	return nil, ErrUnsupported
}

func (a *anthropic) ListModels(ctx context.Context) ([]string, error) {
	obj, err := a.doJSON(ctx, http.MethodGet, a.url("/models?limit=1000"), nil, a.header())
	if err != nil {
		return nil, err
	}
	var ids []string
	list, _ := obj["data"].([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

// MessagesProvider is implemented by adapters that speak the Anthropic
// Messages API natively, so /v1/messages can be passed through unchanged.
type MessagesProvider interface {
	// Messages forwards a Messages request body and returns the raw 2xx
	// response (JSON, or an SSE stream when the body asks to stream).
	Messages(ctx context.Context, body []byte, upstreamModel string, in http.Header) (*http.Response, error)
	// CountTokens forwards a count_tokens request.
	CountTokens(ctx context.Context, body Object, upstreamModel string, in http.Header) (Object, error)
}

// forwardHeader adds the client's anthropic-* headers (API version, beta
// flags, and whatever later capabilities add), which must reach the API
// unchanged. Credentials stay the provider's own.
func (a *anthropic) forwardHeader(in http.Header) http.Header {
	h := a.header()
	for k, v := range in {
		if strings.HasPrefix(strings.ToLower(k), "anthropic-") {
			h[k] = v
		}
	}
	return h
}

// SpliceModel replaces the top-level "model" string of a JSON object without
// re-encoding anything else, so a request reaches the API byte-for-byte
// (prompt caching and thinking-signature checks depend on unchanged content).
func SpliceModel(raw []byte, model string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		afterKey := int(dec.InputOffset())
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if key, _ := t.(string); key != "model" {
			continue
		}
		end := int(dec.InputOffset())
		start := afterKey + bytes.Index(raw[afterKey:end], v)
		enc, _ := json.Marshal(model)
		out := make([]byte, 0, len(raw)+len(enc))
		out = append(append(append(out, raw[:start]...), enc...), raw[start+len(v):]...)
		return out, nil
	}
	return nil, fmt.Errorf("model: field required")
}

func withModel(body Object, model string) Object {
	out := make(Object, len(body))
	for k, v := range body {
		out[k] = v
	}
	out["model"] = model
	return out
}

func (a *anthropic) Messages(ctx context.Context, body []byte, upstreamModel string, in http.Header) (*http.Response, error) {
	b, err := SpliceModel(body, upstreamModel)
	if err != nil {
		return nil, &Error{Status: http.StatusBadRequest, Message: err.Error()}
	}
	return a.doRaw(ctx, http.MethodPost, a.url("/messages"), b, a.forwardHeader(in))
}

func (a *anthropic) CountTokens(ctx context.Context, body Object, upstreamModel string, in http.Header) (Object, error) {
	return a.doJSON(ctx, http.MethodPost, a.url("/messages/count_tokens"), withModel(body, upstreamModel), a.forwardHeader(in))
}
