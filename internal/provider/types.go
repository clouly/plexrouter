// Package provider adapts the OpenAI Chat Completions format (the gateway's
// public API) to each upstream's native API.
//
// Responses and stream chunks are passed around as generic JSON objects
// (Object) already in OpenAI format, so that fields the gateway does not know
// about (e.g. "reasoning_content" from some OpenAI-compatible servers) survive.
package provider

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Object is a decoded JSON object. Numbers decode as json.Number.
type Object = map[string]any

// ChatRequest is the subset of an OpenAI chat request the adapters understand.
// Raw holds the full original body for pass-through to OpenAI-style upstreams.
type ChatRequest struct {
	Model               string          `json:"model"`
	Models              []string        `json:"models"` // gateway extension: fallback list
	Messages            []Message       `json:"messages"`
	Stream              bool            `json:"stream"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	TopK                *int            `json:"top_k"`
	Stop                json.RawMessage `json:"stop"`
	Tools               []Tool          `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ResponseFormat      *ResponseFormat `json:"response_format"`
	User                string          `json:"user"`
	ReasoningEffort     string          `json:"reasoning_effort"` // OpenAI style
	Reasoning           *Reasoning      `json:"reasoning"`        // OpenRouter style

	Raw Object `json:"-"`
}

// Reasoning is OpenRouter's reasoning control.
type Reasoning struct {
	Effort    string `json:"effort"`
	MaxTokens *int   `json:"max_tokens"`
	Exclude   bool   `json:"exclude"`
	Enabled   *bool  `json:"enabled"`
}

// ReasoningRequest returns the requested reasoning effort ("" if none) and
// an explicit thinking-token budget (0 if none).
func (r *ChatRequest) ReasoningRequest() (effort string, budget int) {
	effort = r.ReasoningEffort
	if o := r.Reasoning; o != nil {
		if o.Enabled != nil && !*o.Enabled {
			return "", 0
		}
		if o.Effort != "" {
			effort = o.Effort
		}
		if o.MaxTokens != nil && *o.MaxTokens > 0 {
			budget = *o.MaxTokens
		}
		if effort == "" && (budget > 0 || (o.Enabled != nil && *o.Enabled)) {
			effort = "medium"
		}
	}
	if effort == "none" {
		return "", 0
	}
	return effort, budget
}

// ExcludeReasoning reports whether the client asked not to receive reasoning text.
func (r *ChatRequest) ExcludeReasoning() bool { return r.Reasoning != nil && r.Reasoning.Exclude }

// thinkingBudget turns an effort level into a thinking-token budget.
func thinkingBudget(effort string, explicit int) int {
	if explicit > 0 {
		return explicit
	}
	switch effort {
	case "minimal":
		return 1024
	case "low":
		return 2048
	case "high", "xhigh":
		return 16384
	}
	return 8192 // medium
}

// HasToolHistory reports whether earlier assistant turns called tools.
func (r *ChatRequest) HasToolHistory() bool {
	for _, m := range r.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"` // string, []ContentPart or null
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type ContentPart struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"` // Anthropic prompt-cache breakpoint
	ImageURL     *struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url,omitempty"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ResponseFormat struct {
	Type       string `json:"type"` // text | json_object | json_schema
	JSONSchema *struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
		Strict bool            `json:"strict"`
	} `json:"json_schema,omitempty"`
}

// ParseChatRequest decodes a client request body.
func ParseChatRequest(body []byte) (*ChatRequest, error) {
	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&req.Raw); err != nil {
		return nil, err
	}
	return &req, nil
}

// MaxOutputTokens returns the client's output-token cap, if any.
func (r *ChatRequest) MaxOutputTokens() int {
	if r.MaxCompletionTokens != nil {
		return *r.MaxCompletionTokens
	}
	if r.MaxTokens != nil {
		return *r.MaxTokens
	}
	return 0
}

// StopSequences normalizes "stop" (string or list).
func (r *ChatRequest) StopSequences() []string {
	if len(r.Stop) == 0 || string(r.Stop) == "null" {
		return nil
	}
	var one string
	if json.Unmarshal(r.Stop, &one) == nil {
		return []string{one}
	}
	var many []string
	json.Unmarshal(r.Stop, &many)
	return many
}

// Parts returns message content as parts; a plain string becomes one text part.
func (m *Message) Parts() []ContentPart {
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		if s == "" {
			return nil
		}
		return []ContentPart{{Type: "text", Text: s}}
	}
	var parts []ContentPart
	json.Unmarshal(m.Content, &parts)
	return parts
}

// Text concatenates the text parts of a message.
func (m *Message) Text() string {
	var b strings.Builder
	for _, p := range m.Parts() {
		if p.Type == "text" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// Usage is token accounting reported by an upstream. PromptTokens includes
// tokens read from (CacheReadTokens) and written to (CacheWriteTokens) the
// provider's prompt cache.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
}

// UsageOf extracts OpenAI-style usage from a response or chunk, if present.
func UsageOf(obj Object) *Usage {
	u, ok := obj["usage"].(map[string]any)
	if !ok {
		return nil
	}
	out := &Usage{PromptTokens: toInt(u["prompt_tokens"]), CompletionTokens: toInt(u["completion_tokens"])}
	if d, ok := u["prompt_tokens_details"].(map[string]any); ok {
		out.CacheReadTokens = toInt(d["cached_tokens"])
		out.CacheWriteTokens = toInt(d["cache_write_tokens"])
	}
	if out.CacheReadTokens == 0 {
		out.CacheReadTokens = toInt(u["prompt_cache_hit_tokens"]) // DeepSeek
	}
	return out
}

func toInt(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			f, _ := n.Float64()
			return int(f)
		}
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func usageObject(u Usage) Object {
	obj := Object{"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens,
		"total_tokens": u.PromptTokens + u.CompletionTokens}
	if u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
		obj["prompt_tokens_details"] = Object{"cached_tokens": u.CacheReadTokens, "cache_write_tokens": u.CacheWriteTokens}
	}
	return obj
}
