package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Conversion between the Anthropic Messages API (served at /v1/messages for
// Claude Code and Anthropic SDKs) and the OpenAI chat format the adapters
// speak, for routing Messages requests to non-Anthropic providers.

type anthropicRequest struct {
	Model         string             `json:"model"`
	System        json.RawMessage    `json:"system"` // string or text blocks
	Messages      []anthropicMessage `json:"messages"`
	MaxTokens     *int               `json:"max_tokens"`
	Temperature   *float64           `json:"temperature"`
	TopP          *float64           `json:"top_p"`
	TopK          *int               `json:"top_k"`
	StopSequences []string           `json:"stop_sequences"`
	Stream        bool               `json:"stream"`
	Tools         []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	} `json:"tools"`
	ToolChoice *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tool_choice"`
	Thinking *struct {
		Type         string `json:"type"` // "enabled" (fixed budget) or "adaptive"
		BudgetTokens int    `json:"budget_tokens"`
	} `json:"thinking"`
	OutputConfig *struct {
		Effort string `json:"effort"`
	} `json:"output_config"`
	Metadata *struct {
		UserID string `json:"user_id"`
	} `json:"metadata"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or blocks
}

type anthropicContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Input        json.RawMessage `json:"input"`
	ToolUseID    string          `json:"tool_use_id"`
	Content      json.RawMessage `json:"content"` // tool_result: string or blocks
	IsError      bool            `json:"is_error"`
	CacheControl json.RawMessage `json:"cache_control"`
	Source       *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	} `json:"source"`
}

// blocks reads content that may be a plain string or a list of blocks.
func blocks(raw json.RawMessage) []anthropicContentBlock {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []anthropicContentBlock{{Type: "text", Text: s}}
	}
	var out []anthropicContentBlock
	json.Unmarshal(raw, &out)
	return out
}

func blocksText(bs []anthropicContentBlock) string {
	var parts []string
	for _, b := range bs {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func textPart(b anthropicContentBlock) Object {
	p := Object{"type": "text", "text": b.Text}
	if len(b.CacheControl) > 0 && string(b.CacheControl) != "null" {
		p["cache_control"] = b.CacheControl
	}
	return p
}

// AnthropicToChat converts a Messages API request body to a chat request.
// It returns the request's model name as sent.
func AnthropicToChat(body []byte) (*ChatRequest, string, error) {
	var in anthropicRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, "", err
	}
	var msgs []any
	if sys := blocks(in.System); len(sys) > 0 {
		parts := []any{}
		for _, b := range sys {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, textPart(b))
			}
		}
		msgs = append(msgs, Object{"role": "system", "content": parts})
	}
	for _, m := range in.Messages {
		bs := blocks(m.Content)
		if m.Role == "assistant" {
			var text strings.Builder
			var calls []any
			for _, b := range bs {
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
				case "tool_use":
					args := string(b.Input)
					if args == "" || args == "null" {
						args = "{}"
					}
					calls = append(calls, Object{"id": b.ID, "type": "function", "function": Object{"name": b.Name, "arguments": args}})
				}
				// thinking / redacted_thinking blocks have no OpenAI equivalent.
			}
			msg := Object{"role": "assistant", "content": text.String()}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			msgs = append(msgs, msg)
			continue
		}
		// User turn: tool results become tool messages (which must directly
		// follow the assistant's tool calls), the rest one user message.
		var parts []any
		for _, b := range bs {
			switch b.Type {
			case "tool_result":
				content := blocksText(blocks(b.Content))
				if b.IsError && content != "" {
					content = "Error: " + content
				}
				msgs = append(msgs, Object{"role": "tool", "tool_call_id": b.ToolUseID, "content": content})
			case "text":
				if b.Text != "" {
					parts = append(parts, textPart(b))
				}
			case "image":
				if b.Source == nil {
					continue
				}
				url := b.Source.URL
				if b.Source.Type == "base64" {
					url = "data:" + b.Source.MediaType + ";base64," + b.Source.Data
				}
				parts = append(parts, Object{"type": "image_url", "image_url": Object{"url": url}})
			}
		}
		if len(parts) > 0 {
			msgs = append(msgs, Object{"role": "user", "content": parts})
		}
	}

	out := Object{"model": in.Model, "messages": msgs, "stream": in.Stream}
	if in.MaxTokens != nil {
		out["max_tokens"] = *in.MaxTokens
	}
	if in.Temperature != nil {
		out["temperature"] = *in.Temperature
	}
	if in.TopP != nil {
		out["top_p"] = *in.TopP
	}
	if in.TopK != nil {
		out["top_k"] = *in.TopK
	}
	if len(in.StopSequences) > 0 {
		out["stop"] = in.StopSequences
	}
	var tools []any
	for _, t := range in.Tools {
		if len(t.InputSchema) == 0 { // Anthropic server tools (web search…) can't be forwarded
			continue
		}
		tools = append(tools, Object{"type": "function", "function": Object{"name": t.Name, "description": t.Description, "parameters": t.InputSchema}})
	}
	if len(tools) > 0 {
		out["tools"] = tools
		if tc := in.ToolChoice; tc != nil {
			switch tc.Type {
			case "auto":
				out["tool_choice"] = "auto"
			case "any":
				out["tool_choice"] = "required"
			case "none":
				out["tool_choice"] = "none"
			case "tool":
				out["tool_choice"] = Object{"type": "function", "function": Object{"name": tc.Name}}
			}
		}
	}
	if th := in.Thinking; th != nil && (th.Type == "enabled" || th.Type == "adaptive") {
		r := Object{"enabled": true}
		if th.Type == "enabled" && th.BudgetTokens > 0 {
			r["max_tokens"] = th.BudgetTokens
		}
		if oc := in.OutputConfig; oc != nil && oc.Effort != "" {
			r["effort"] = oc.Effort
		}
		out["reasoning"] = r
	}
	if in.Metadata != nil && in.Metadata.UserID != "" {
		out["user"] = in.Metadata.UserID
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, "", err
	}
	req, err := ParseChatRequest(b)
	return req, in.Model, err
}

func anthropicStopReason(finish any) string {
	switch finish {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	}
	return "end_turn"
}

// AnthropicUsage converts usage to the Messages API shape, where
// input_tokens excludes cache reads and writes.
func AnthropicUsage(u *Usage) Object {
	if u == nil {
		return Object{"input_tokens": 0, "output_tokens": 0}
	}
	return Object{
		"input_tokens":                max(u.PromptTokens-u.CacheReadTokens-u.CacheWriteTokens, 0),
		"output_tokens":               u.CompletionTokens,
		"cache_read_input_tokens":     u.CacheReadTokens,
		"cache_creation_input_tokens": u.CacheWriteTokens,
	}
}

func parseArgs(s string) json.RawMessage {
	if s == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}

// ChatToAnthropic converts a chat completion to a Messages API response.
func ChatToAnthropic(resp Object, model string) Object {
	var content []any
	var finish any
	if choices, _ := resp["choices"].([]any); len(choices) > 0 {
		c, _ := choices[0].(map[string]any)
		finish = c["finish_reason"]
		msg, _ := c["message"].(map[string]any)
		for _, f := range []string{"reasoning", "reasoning_content"} {
			if r, _ := msg[f].(string); r != "" {
				content = append(content, Object{"type": "thinking", "thinking": r, "signature": ""})
				break
			}
		}
		if t, _ := msg["content"].(string); t != "" {
			content = append(content, Object{"type": "text", "text": t})
		}
		calls, _ := msg["tool_calls"].([]any)
		for _, tc := range calls {
			call, _ := tc.(map[string]any)
			fn, _ := call["function"].(map[string]any)
			name, _ := fn["name"].(string)
			args, _ := fn["arguments"].(string)
			id, _ := call["id"].(string)
			content = append(content, Object{"type": "tool_use", "id": id, "name": name, "input": parseArgs(args)})
		}
	}
	if content == nil {
		content = []any{}
	}
	id, _ := resp["id"].(string)
	if !strings.HasPrefix(id, "msg_") {
		id = "msg_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	return Object{
		"id": id, "type": "message", "role": "assistant", "model": model, "content": content,
		"stop_reason": anthropicStopReason(finish), "stop_sequence": nil, "usage": AnthropicUsage(UsageOf(resp)),
	}
}

// AnthropicEvent is one server-sent event of a Messages stream.
type AnthropicEvent struct {
	Name string
	Data Object
}

// AnthropicStreamConverter turns chat completion chunks into Messages API
// stream events.
type AnthropicStreamConverter struct {
	model     string
	started   bool
	block     int    // index of the open content block, -1 if none
	blockKind string // "thinking", "text" or "tool:<chat tool index>"
	nextIndex int
	finish    any
	usage     *Usage
}

func NewAnthropicStreamConverter(model string) *AnthropicStreamConverter {
	return &AnthropicStreamConverter{model: model, block: -1}
}

func (c *AnthropicStreamConverter) start(id string) []AnthropicEvent {
	if c.started {
		return nil
	}
	c.started = true
	if !strings.HasPrefix(id, "msg_") {
		id = "msg_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	return []AnthropicEvent{{"message_start", Object{"type": "message_start", "message": Object{
		"id": id, "type": "message", "role": "assistant", "model": c.model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": Object{"input_tokens": 0, "output_tokens": 0},
	}}}}
}

// open switches to a content block of the given kind, closing the current one.
func (c *AnthropicStreamConverter) open(kind string, block Object) []AnthropicEvent {
	if c.block >= 0 && c.blockKind == kind {
		return nil
	}
	evs := c.closeBlock()
	c.block, c.blockKind = c.nextIndex, kind
	c.nextIndex++
	return append(evs, AnthropicEvent{"content_block_start", Object{"type": "content_block_start", "index": c.block, "content_block": block}})
}

func (c *AnthropicStreamConverter) closeBlock() []AnthropicEvent {
	if c.block < 0 {
		return nil
	}
	ev := AnthropicEvent{"content_block_stop", Object{"type": "content_block_stop", "index": c.block}}
	c.block = -1
	return []AnthropicEvent{ev}
}

func (c *AnthropicStreamConverter) delta(d Object) AnthropicEvent {
	return AnthropicEvent{"content_block_delta", Object{"type": "content_block_delta", "index": c.block, "delta": d}}
}

// Chunk converts one chat completion chunk.
func (c *AnthropicStreamConverter) Chunk(ch Object) []AnthropicEvent {
	id, _ := ch["id"].(string)
	evs := c.start(id)
	if u := UsageOf(ch); u != nil {
		c.usage = u
	}
	choices, _ := ch["choices"].([]any)
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		if f := choice["finish_reason"]; f != nil {
			c.finish = f
		}
		d, _ := choice["delta"].(map[string]any)
		for _, f := range []string{"reasoning", "reasoning_content"} {
			if r, _ := d[f].(string); r != "" {
				evs = append(evs, c.open("thinking", Object{"type": "thinking", "thinking": "", "signature": ""})...)
				evs = append(evs, c.delta(Object{"type": "thinking_delta", "thinking": r}))
				break
			}
		}
		if t, _ := d["content"].(string); t != "" {
			evs = append(evs, c.open("text", Object{"type": "text", "text": ""})...)
			evs = append(evs, c.delta(Object{"type": "text_delta", "text": t}))
		}
		calls, _ := d["tool_calls"].([]any)
		for _, raw := range calls {
			call, _ := raw.(map[string]any)
			fn, _ := call["function"].(map[string]any)
			kind := fmt.Sprintf("tool:%v", call["index"])
			if c.block < 0 || c.blockKind != kind {
				id, _ := call["id"].(string)
				name, _ := fn["name"].(string)
				evs = append(evs, c.open(kind, Object{"type": "tool_use", "id": id, "name": name, "input": Object{}})...)
			}
			if args, _ := fn["arguments"].(string); args != "" {
				evs = append(evs, c.delta(Object{"type": "input_json_delta", "partial_json": args}))
			}
		}
	}
	return evs
}

// Finish ends the message after the last chunk.
func (c *AnthropicStreamConverter) Finish() []AnthropicEvent {
	evs := c.start("")
	evs = append(evs, c.closeBlock()...)
	return append(evs,
		AnthropicEvent{"message_delta", Object{"type": "message_delta",
			"delta": Object{"stop_reason": anthropicStopReason(c.finish), "stop_sequence": nil}, "usage": AnthropicUsage(c.usage)}},
		AnthropicEvent{"message_stop", Object{"type": "message_stop"}})
}

// Usage returns the usage reported in the stream, if any.
func (c *AnthropicStreamConverter) Usage() *Usage { return c.usage }
