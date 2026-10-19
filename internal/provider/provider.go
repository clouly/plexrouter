package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	TypeOpenAI           = "openai"
	TypeAnthropic        = "anthropic"
	TypeGemini           = "gemini"
	TypeOpenAICompatible = "openai_compatible"
)

var Types = []string{TypeOpenAI, TypeAnthropic, TypeGemini, TypeOpenAICompatible}

// DefaultBaseURL returns the base URL used when a provider leaves it empty.
func DefaultBaseURL(typ string) string {
	switch typ {
	case TypeOpenAI:
		return "https://api.openai.com/v1"
	case TypeAnthropic:
		return "https://api.anthropic.com"
	case TypeGemini:
		return "https://generativelanguage.googleapis.com"
	}
	return ""
}

// Adapter talks to one upstream account.
type Adapter interface {
	// Chat performs a non-streaming completion and returns an OpenAI-format response.
	Chat(ctx context.Context, req *ChatRequest, upstreamModel string) (Object, error)
	// ChatStream starts a streaming completion. It returns only after the
	// upstream accepted the request, so callers can still fall back on error.
	ChatStream(ctx context.Context, req *ChatRequest, upstreamModel string) (Stream, error)
	// Embeddings forwards an OpenAI embeddings request.
	Embeddings(ctx context.Context, body Object, upstreamModel string) (Object, error)
	// ListModels returns the model IDs the upstream offers.
	ListModels(ctx context.Context) ([]string, error)
}

// Stream yields OpenAI-format chat.completion.chunk objects.
type Stream interface {
	// Next returns the next chunk, or io.EOF when the stream is done.
	Next() (Object, error)
	Close() error
}

type Config struct {
	Type    string
	BaseURL string
	APIKey  string
}

func New(cfg Config, client *http.Client) (Adapter, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL(cfg.Type)
	}
	if base == "" {
		return nil, fmt.Errorf("provider type %q needs a base URL", cfg.Type)
	}
	h := httpClient{client: client, base: base, key: cfg.APIKey}
	switch cfg.Type {
	case TypeOpenAI, TypeOpenAICompatible:
		return &openAI{h}, nil
	case TypeAnthropic:
		return &anthropic{h}, nil
	case TypeGemini:
		return &gemini{h}, nil
	}
	return nil, fmt.Errorf("unknown provider type %q", cfg.Type)
}

// Error is a failed upstream call. Status 0 means the request never got an
// HTTP response (network error, timeout).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return e.Message
	}
	return fmt.Sprintf("upstream %d: %s", e.Status, e.Message)
}

// Retryable reports whether another endpoint might succeed where this failed.
// Errors that are the client's fault (malformed request) are not retried.
func Retryable(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		switch e.Status {
		case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
			return false
		}
		return true
	}
	return !errors.Is(err, context.Canceled)
}

var ErrUnsupported = &Error{Status: http.StatusNotImplemented, Message: "operation not supported by this provider type"}

type httpClient struct {
	client *http.Client
	base   string
	key    string
}

// do sends a request and returns the response if the status is 2xx; otherwise
// it reads the body into an *Error.
func (h httpClient) do(ctx context.Context, method, url string, body any, header http.Header) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Message: err.Error()}
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, &Error{Status: resp.StatusCode, Message: errorMessage(b)}
	}
	return resp, nil
}

func (h httpClient) doJSON(ctx context.Context, method, url string, body any, header http.Header) (Object, error) {
	resp, err := h.do(ctx, method, url, body, header)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeObject(resp.Body)
}

func decodeObject(r io.Reader) (Object, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var obj Object
	if err := dec.Decode(&obj); err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Message: "invalid JSON from upstream: " + err.Error()}
	}
	return obj, nil
}

// errorMessage pulls a readable message out of the common error shapes
// ({"error":{"message":..}}, {"error":"..."}, {"message":..}).
func errorMessage(body []byte) string {
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &v) == nil {
		var inner struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(v.Error, &inner) == nil && inner.Message != "" {
			return inner.Message
		}
		var s string
		if json.Unmarshal(v.Error, &s) == nil && s != "" {
			return s
		}
		if v.Message != "" {
			return v.Message
		}
	}
	// Some upstreams wrap errors in an array (Gemini streaming).
	var arr []json.RawMessage
	if json.Unmarshal(body, &arr) == nil && len(arr) > 0 {
		return errorMessage(arr[0])
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	if s == "" {
		s = "empty response"
	}
	return s
}

// sseReader reads "data:" payloads from a server-sent event stream.
type sseReader struct {
	r    *bufio.Reader
	body io.Closer
}

func newSSEReader(body io.ReadCloser) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(body, 64<<10), body: body}
}

// next returns the next data payload, or io.EOF.
func (s *sseReader) next() ([]byte, error) {
	var data []byte
	for {
		line, err := s.r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if len(data) > 0 {
				return data, nil
			}
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, &Error{Message: "stream interrupted: " + err.Error()}
		}
		line = bytes.TrimRight(line, "\r\n")
		switch {
		case len(line) == 0: // event boundary
			if len(data) > 0 {
				return data, nil
			}
		case bytes.HasPrefix(line, []byte("data:")):
			payload := bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" "))
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, payload...)
		}
		// "event:", "id:", ":" comments are ignored; payloads carry their own type.
		if err != nil && len(data) > 0 {
			return data, nil
		}
	}
}

func (s *sseReader) Close() error { return s.body.Close() }

func decodeChunk(data []byte) (Object, error) {
	return decodeObject(bytes.NewReader(data))
}

func chunk(id, model string, created int64, delta Object, finish any) Object {
	return Object{
		"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{Object{"index": 0, "delta": delta, "finish_reason": finish}},
	}
}

func completion(id, model string, created int64, message Object, finish any, usage Object) Object {
	return Object{
		"id": id, "object": "chat.completion", "created": created, "model": model,
		"choices": []any{Object{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   usage,
	}
}

func unixNow() int64 { return time.Now().Unix() }
