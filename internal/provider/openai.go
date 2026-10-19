package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
)

// openAI serves OpenAI and any OpenAI-compatible API (DeepSeek, Groq,
// Together, Mistral, vLLM, Ollama, LM Studio…) by passing the body through.
type openAI struct {
	httpClient
	// official is the OpenAI API itself, which rejects fields it doesn't
	// know; compatible servers get the body as sent (some
	// understand "reasoning" and "cache_control").
	official bool
}

// Fields that belong to the gateway, not the upstream.
var gatewayFields = []string{"models", "route", "provider", "transforms", "usage", "plugins"}

func (o *openAI) header() http.Header {
	h := http.Header{}
	if o.key != "" {
		h.Set("Authorization", "Bearer "+o.key)
	}
	return h
}

func (o *openAI) body(req *ChatRequest, upstreamModel string, stream bool) Object {
	body := make(Object, len(req.Raw)+1)
	for k, v := range req.Raw {
		body[k] = v
	}
	for _, f := range gatewayFields {
		delete(body, f)
	}
	body["model"] = upstreamModel
	if o.official {
		if effort, _ := req.ReasoningRequest(); effort != "" && req.ReasoningEffort == "" {
			if effort == "xhigh" {
				effort = "high"
			}
			body["reasoning_effort"] = effort
		}
		delete(body, "reasoning")
		body["messages"] = stripCacheControl(body["messages"])
	}
	if stream {
		body["stream"] = true
		opts, _ := body["stream_options"].(map[string]any)
		if opts == nil {
			opts = Object{}
		}
		opts["include_usage"] = true
		body["stream_options"] = opts
	} else {
		delete(body, "stream")
		delete(body, "stream_options")
	}
	return body
}

// stripCacheControl removes Anthropic-style cache_control markers from
// message content parts.
func stripCacheControl(messages any) any {
	list, ok := messages.([]any)
	if !ok {
		return messages
	}
	out := make([]any, len(list))
	for i, m := range list {
		msg, ok := m.(map[string]any)
		parts, isList := msg["content"].([]any)
		if !ok || !isList {
			out[i] = m
			continue
		}
		cp := make(Object, len(msg))
		for k, v := range msg {
			cp[k] = v
		}
		newParts := make([]any, len(parts))
		for j, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if _, has := pm["cache_control"]; has {
					q := make(Object, len(pm))
					for k, v := range pm {
						if k != "cache_control" {
							q[k] = v
						}
					}
					p = q
				}
			}
			newParts[j] = p
		}
		cp["content"] = newParts
		out[i] = cp
	}
	return out
}

func (o *openAI) Chat(ctx context.Context, req *ChatRequest, upstreamModel string) (Object, error) {
	return o.doJSON(ctx, http.MethodPost, o.base+"/chat/completions", o.body(req, upstreamModel, false), o.header())
}

func (o *openAI) ChatStream(ctx context.Context, req *ChatRequest, upstreamModel string) (Stream, error) {
	resp, err := o.do(ctx, http.MethodPost, o.base+"/chat/completions", o.body(req, upstreamModel, true), o.header())
	if err != nil {
		return nil, err
	}
	return &openAIStream{sse: newSSEReader(resp.Body)}, nil
}

type openAIStream struct{ sse *sseReader }

func (s *openAIStream) Next() (Object, error) {
	for {
		data, err := s.sse.next()
		if err != nil {
			return nil, err
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			continue
		}
		if string(data) == "[DONE]" {
			return nil, io.EOF
		}
		obj, err := decodeChunk(data)
		if err != nil {
			return nil, err
		}
		if e, ok := obj["error"]; ok && e != nil {
			return nil, &Error{Status: http.StatusBadGateway, Message: errorMessage(data)}
		}
		return obj, nil
	}
}

func (s *openAIStream) Close() error { return s.sse.Close() }

func (o *openAI) Embeddings(ctx context.Context, body Object, upstreamModel string) (Object, error) {
	out := make(Object, len(body))
	for k, v := range body {
		out[k] = v
	}
	for _, f := range gatewayFields {
		delete(out, f)
	}
	out["model"] = upstreamModel
	return o.doJSON(ctx, http.MethodPost, o.base+"/embeddings", out, o.header())
}

func (o *openAI) ListModels(ctx context.Context) ([]string, error) {
	obj, err := o.doJSON(ctx, http.MethodGet, o.base+"/models", nil, o.header())
	if err != nil {
		return nil, err
	}
	var ids []string
	// OpenAI uses {"data":[...]}; Ollama-style servers may use {"models":[...]}.
	for _, key := range []string{"data", "models"} {
		list, _ := obj[key].([]any)
		for _, item := range list {
			m, _ := item.(map[string]any)
			for _, f := range []string{"id", "name", "model"} {
				if id, ok := m[f].(string); ok && id != "" {
					ids = append(ids, id)
					break
				}
			}
		}
	}
	return ids, nil
}
