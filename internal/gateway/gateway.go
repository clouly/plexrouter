// Package gateway serves the OpenAI-compatible API: it authenticates API keys,
// routes a model slug to its provider endpoints (falling back on failure),
// relays the response, and bills token usage.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local4a/plexrouter/internal/provider"
	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/store"
)

const (
	KeyPrefix    = "sk-plex-"
	maxBodyBytes = 50 << 20 // requests may carry base64 images
)

type Gateway struct {
	store   *store.Store
	box     *secure.Box
	client  *http.Client
	opts    Options
	log     *slog.Logger
	health  *health
	limiter *rateLimiter
}

type Options struct {
	Timeout        time.Duration // upper bound for one upstream request
	DefaultUserRPM int           // requests/minute per user unless set on the user; 0 = unlimited
}

func New(st *store.Store, box *secure.Box, opts Options, log *slog.Logger) *Gateway {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &Gateway{store: st, box: box, client: &http.Client{Transport: transport}, opts: opts, log: log,
		health: newHealth(), limiter: newRateLimiter()}
}

// Register mounts the API under /v1 and /api/v1 (OpenRouter-style base URL).
func (g *Gateway) Register(mux *http.ServeMux) {
	for _, p := range []string{"/v1", "/api/v1"} {
		mux.HandleFunc("POST "+p+"/chat/completions", g.handleChat)
		mux.HandleFunc("POST "+p+"/embeddings", g.handleEmbeddings)
		mux.HandleFunc("POST "+p+"/messages", g.handleMessages)
		mux.HandleFunc("POST "+p+"/messages/count_tokens", g.handleCountTokens)
		mux.HandleFunc("GET "+p+"/models", g.handleModels)
		mux.HandleFunc("GET "+p+"/key", g.handleKey)
		mux.HandleFunc(p+"/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "unknown endpoint "+r.Method+" "+r.URL.Path)
		})
	}
}

// --- errors ---

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message, "type": code, "code": code}})
}

// errorInfo maps an error to an HTTP status, code and message.
func errorInfo(w http.ResponseWriter, err error) (status int, code, message string) {
	var rl *rateLimited
	if errors.As(err, &rl) {
		w.Header().Set("Retry-After", strconv.Itoa(int(rl.wait.Seconds())+1))
		return http.StatusTooManyRequests, "rate_limited", rl.Error()
	}
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status, ae.code, ae.message
	}
	var pe *provider.Error
	if errors.As(err, &pe) {
		status := pe.Status
		if status == 0 || status < 400 || status == http.StatusUnauthorized || status == http.StatusForbidden {
			// Network failures and upstream auth problems are the gateway's
			// problem, not the client's.
			status = http.StatusBadGateway
		}
		return status, "upstream_error", pe.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "timeout", "upstream request timed out"
	}
	return http.StatusBadGateway, "upstream_error", err.Error()
}

func writeAPIError(w http.ResponseWriter, err error) {
	status, code, message := errorInfo(w, err)
	writeError(w, status, code, message)
}

// writeAnthropicError writes an error in the Messages API shape. An error
// that an Anthropic upstream returned for the request itself is forwarded
// unmodified: clients such as Claude Code recover from capability
// rejections by matching the upstream's wording.
func writeAnthropicError(w http.ResponseWriter, err error) {
	var pe *provider.Error
	if errors.As(err, &pe) && pe.Status >= 400 && pe.Status != http.StatusUnauthorized && pe.Status != http.StatusForbidden &&
		pe.Status != http.StatusPaymentRequired && isAnthropicError(pe.Body) {
		for _, h := range []string{"Retry-After", "X-Should-Retry", "Request-Id"} {
			if v := pe.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(pe.Status)
		w.Write(pe.Body)
		return
	}
	status, _, message := errorInfo(w, err)
	typ := "api_error"
	switch status {
	case http.StatusBadRequest, http.StatusNotFound:
		typ = map[int]string{400: "invalid_request_error", 404: "not_found_error"}[status]
	case http.StatusUnauthorized:
		typ = "authentication_error"
	case http.StatusPaymentRequired:
		typ = "billing_error"
	case http.StatusForbidden:
		typ = "permission_error"
	case http.StatusRequestEntityTooLarge:
		typ = "request_too_large"
	case http.StatusTooManyRequests:
		typ = "rate_limit_error"
	case http.StatusServiceUnavailable, 529:
		typ = "overloaded_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}})
}

// isAnthropicError reports whether a body is a Messages API error object.
func isAnthropicError(b []byte) bool {
	var v struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	return json.Unmarshal(b, &v) == nil && v.Type == "error" && len(v.Error) > 0
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// --- auth & routing ---

type caller struct {
	api  string // gateway endpoint being served: "chat", "messages" or "embeddings"
	key  *store.APIKey
	user *store.User
}

func (g *Gateway) authenticate(r *http.Request) (*caller, error) {
	token := strings.TrimSpace(r.Header.Get("Authorization"))
	token, _ = strings.CutPrefix(token, "Bearer ")
	if token == "" {
		token = r.Header.Get("x-api-key")
	}
	if !strings.HasPrefix(token, KeyPrefix) {
		return nil, &apiError{http.StatusUnauthorized, "invalid_api_key", "missing or invalid API key"}
	}
	key, user, err := g.store.KeyAuth(r.Context(), secure.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, &apiError{http.StatusUnauthorized, "invalid_api_key", "invalid API key"}
	}
	if err != nil {
		return nil, err
	}
	if key.Disabled {
		return nil, &apiError{http.StatusUnauthorized, "key_disabled", "this API key is disabled"}
	}
	if user.Disabled {
		return nil, &apiError{http.StatusForbidden, "account_disabled", "this account is disabled"}
	}
	return &caller{key: key, user: user}, nil
}

// checkFunds rejects paid models when the account or key has nothing left.
func (c *caller) checkFunds(m *store.Model) error {
	if m.InputPrice == 0 && m.OutputPrice == 0 {
		return nil
	}
	if c.key.CreditLimit != nil && c.key.Usage >= *c.key.CreditLimit {
		return &apiError{http.StatusPaymentRequired, "key_limit_reached", "this API key reached its credit limit"}
	}
	if c.user.Balance <= 0 {
		return &apiError{http.StatusPaymentRequired, "insufficient_credits", "insufficient credits; add credits to your account"}
	}
	return nil
}

type candidate struct {
	model    *store.Model
	endpoint store.Endpoint
}

// candidates expands requested slugs (primary + fallbacks) into the ordered
// list of endpoints to try.
func (g *Gateway) candidates(ctx context.Context, slugs []string) ([]candidate, error) {
	var out []candidate
	seen := map[string]bool{}
	var unknown []string
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		m, err := g.store.RouteModel(ctx, slug)
		if errors.Is(err, store.ErrNotFound) {
			m, err = g.resolveAlias(ctx, slug)
		}
		if errors.Is(err, store.ErrNotFound) {
			unknown = append(unknown, slug)
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range m.Endpoints {
			out = append(out, candidate{model: m, endpoint: e})
		}
	}
	if len(out) == 0 {
		if len(seen) == 0 {
			return nil, &apiError{http.StatusBadRequest, "invalid_request", "\"model\" is required"}
		}
		if len(unknown) > 0 {
			return nil, &apiError{http.StatusNotFound, "model_not_found", fmt.Sprintf("model %q not found", unknown[0])}
		}
		return nil, &apiError{http.StatusServiceUnavailable, "no_endpoints", "no provider is currently available for this model"}
	}
	return out, nil
}

// --- billing ---

func tokenUsage(u *provider.Usage) store.TokenUsage {
	return store.TokenUsage{Prompt: u.PromptTokens, Completion: u.CompletionTokens,
		CacheRead: u.CacheReadTokens, CacheWrite: u.CacheWriteTokens}
}

func setUsageCost(obj provider.Object, nano int64) {
	if u, ok := obj["usage"].(map[string]any); ok {
		u["cost"] = store.ToUSD(nano)
	}
}

// estimatePrompt approximates prompt tokens from message text (images and
// tool schemas are ignored), for upstreams that report no usage.
func estimatePrompt(req *provider.ChatRequest) int {
	n := 0
	for i := range req.Messages {
		n += len(req.Messages[i].Text()) + 16
	}
	return estimateTokens(n)
}

// estimateTokens is a rough fallback for upstreams that report no usage.
func estimateTokens(bytes int) int {
	return (bytes + 3) / 4
}

// record logs one upstream attempt and charges its cost. Failed attempts
// cost nothing, for the user or the operator.
func (g *Gateway) record(c *caller, rt *route, u store.TokenUsage, nano int64, stream bool, callErr error) {
	l := store.RequestLog{
		UserID: c.user.ID, APIKeyID: c.key.ID, APIKeyName: c.key.Name, API: c.api,
		Model: rt.cand.model.Slug, ProviderID: rt.provider.ID, ProviderName: rt.provider.Name,
		PromptTokens: u.Prompt, CompletionTokens: u.Completion, CacheReadTokens: u.CacheRead, CacheWriteTokens: u.CacheWrite,
		Cost:      nano,
		LatencyMS: time.Since(rt.started).Milliseconds(), Status: "success", Stream: stream,
	}
	if rt.key != nil {
		l.ProviderKeyID, l.ProviderKeyName = rt.key.ID, rt.key.Name
	}
	if callErr != nil {
		zero := int64(0)
		l.UpstreamCost = &zero
		l.Status, l.Error = "error", callErr.Error()
		if len(l.Error) > 1000 {
			l.Error = l.Error[:1000]
		}
	} else {
		l.UpstreamCost = rt.cand.endpoint.UpstreamCost(u)
	}
	// Record even if the client went away mid-request.
	if err := g.store.RecordRequest(context.Background(), l); err != nil {
		g.log.Error("record request", "err", err)
	}
	c.user.Balance -= nano
	c.key.Usage += nano
}

// --- handlers ---

func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	c, err := g.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	c.api = "chat"
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request", "request body too large or unreadable")
		return
	}
	req, err := provider.ParseChatRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "\"messages\" must not be empty")
		return
	}
	if err := g.checkLimits(c); err != nil {
		writeAPIError(w, err)
		return
	}
	slugs, err := c.allowedSlugs(append([]string{req.Model}, req.Models...))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	cands, err := g.candidates(r.Context(), slugs)
	if err != nil {
		writeAPIError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), g.opts.Timeout)
	defer cancel()
	promptEstimate := estimatePrompt(req)

	err = g.dispatch(ctx, c, cands, req.Stream, func(rt *route) error {
		if req.Stream {
			stream, err := rt.adapter.ChatStream(ctx, req, rt.cand.endpoint.UpstreamModel)
			if err != nil {
				return err
			}
			g.relayStream(w, c, rt, stream, promptEstimate, req.ExcludeReasoning())
			return nil
		}
		resp, err := rt.adapter.Chat(ctx, req, rt.cand.endpoint.UpstreamModel)
		if err != nil {
			return err
		}
		usage := store.TokenUsage{Prompt: promptEstimate, Completion: estimateTokens(len(contentOf(resp)))}
		if u := provider.UsageOf(resp); u != nil {
			usage = tokenUsage(u)
		}
		nano := rt.cand.model.Charge(usage)
		resp["model"] = rt.cand.model.Slug
		resp["provider"] = rt.provider.Name
		setUsageCost(resp, nano)
		if req.ExcludeReasoning() {
			stripReasoning(resp)
		}
		writeJSON(w, resp)
		g.record(c, rt, usage, nano, false, nil)
		return nil
	})
	if err != nil {
		writeAPIError(w, err)
	}
}

// stripReasoning removes reasoning text from a response or chunk, for
// clients that asked for reasoning with exclude: true.
func stripReasoning(obj provider.Object) {
	choices, _ := obj["choices"].([]any)
	for _, ch := range choices {
		m, _ := ch.(map[string]any)
		for _, key := range []string{"message", "delta"} {
			if msg, ok := m[key].(map[string]any); ok {
				delete(msg, "reasoning")
				delete(msg, "reasoning_content")
				delete(msg, "reasoning_details")
			}
		}
	}
}

// relayStream copies chunks to the client as server-sent events. Once the
// first byte is written there is no falling back, so errors are reported
// in-band as an "error" event.
func (g *Gateway) relayStream(w http.ResponseWriter, c *caller, rt *route, stream provider.Stream, promptEstimate int, excludeReasoning bool) {
	cand := rt.cand
	defer stream.Close()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var usage *provider.Usage
	var outBytes int
	var streamErr error
	for {
		ch, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			streamErr = err
			b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "code": "upstream_error"}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			break
		}
		outBytes += len(contentOf(ch))
		if u := provider.UsageOf(ch); u != nil {
			usage = u
			setUsageCost(ch, cand.model.Charge(tokenUsage(u)))
		}
		ch["model"] = cand.model.Slug
		ch["provider"] = rt.provider.Name
		if excludeReasoning {
			stripReasoning(ch)
		}
		b, err := json.Marshal(ch)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			streamErr = errors.New("client disconnected")
			break
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if streamErr == nil {
		io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	tu := store.TokenUsage{Prompt: promptEstimate, Completion: estimateTokens(outBytes)}
	if usage != nil {
		tu = tokenUsage(usage)
	}
	g.record(c, rt, tu, cand.model.Charge(tu), true, streamErr)
}

// contentOf returns the generated text in a response or chunk, used to
// estimate tokens when the upstream reports no usage.
func contentOf(obj provider.Object) string {
	var b strings.Builder
	choices, _ := obj["choices"].([]any)
	for _, ch := range choices {
		m, _ := ch.(map[string]any)
		for _, key := range []string{"message", "delta"} {
			msg, _ := m[key].(map[string]any)
			for _, f := range []string{"content", "reasoning", "reasoning_content"} {
				if s, ok := msg[f].(string); ok {
					b.WriteString(s)
				}
			}
			calls, _ := msg["tool_calls"].([]any)
			for _, tc := range calls {
				fn, _ := tc.(map[string]any)["function"].(map[string]any)
				if s, ok := fn["arguments"].(string); ok {
					b.WriteString(s)
				}
			}
		}
	}
	return b.String()
}

func (g *Gateway) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	c, err := g.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	c.api = "embeddings"
	var body provider.Object
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := g.checkLimits(c); err != nil {
		writeAPIError(w, err)
		return
	}
	slug, _ := body["model"].(string)
	slugs, err := c.allowedSlugs([]string{slug})
	if err != nil {
		writeAPIError(w, err)
		return
	}
	cands, err := g.candidates(r.Context(), slugs)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), g.opts.Timeout)
	defer cancel()

	err = g.dispatch(ctx, c, cands, false, func(rt *route) error {
		resp, err := rt.adapter.Embeddings(ctx, body, rt.cand.endpoint.UpstreamModel)
		if err != nil {
			return err
		}
		var usage store.TokenUsage
		if u := provider.UsageOf(resp); u != nil {
			usage.Prompt = u.PromptTokens
		}
		nano := rt.cand.model.Charge(usage)
		resp["model"] = rt.cand.model.Slug
		resp["provider"] = rt.provider.Name
		setUsageCost(resp, nano)
		writeJSON(w, resp)
		g.record(c, rt, usage, nano, false, nil)
		return nil
	})
	if err != nil {
		writeAPIError(w, err)
	}
}

func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := g.store.ListModels(r.Context(), true)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	data := []any{}
	for _, m := range models {
		if len(m.Endpoints) == 0 {
			continue
		}
		owner, _, _ := strings.Cut(m.Slug, "/")
		pricing := map[string]string{"prompt": perToken(m.InputPrice), "completion": perToken(m.OutputPrice)}
		if m.CacheReadPrice != nil {
			pricing["input_cache_read"] = perToken(*m.CacheReadPrice)
		}
		if m.CacheWritePrice != nil {
			pricing["input_cache_write"] = perToken(*m.CacheWritePrice)
		}
		data = append(data, map[string]any{
			"id": m.Slug, "object": "model", "created": m.CreatedAt.Unix(), "owned_by": owner,
			"name": m.Name, "display_name": m.Name, // display_name: Anthropic-style, read by Claude Code's model discovery
			"description": m.Description, "context_length": m.ContextLength, "pricing": pricing,
		})
	}
	writeJSON(w, map[string]any{"object": "list", "data": data})
}

func perToken(perMillion float64) string {
	return strconv.FormatFloat(perMillion/1e6, 'f', -1, 64)
}

func (g *Gateway) handleKey(w http.ResponseWriter, r *http.Request) {
	c, err := g.authenticate(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var limit any
	if c.key.CreditLimit != nil {
		limit = store.ToUSD(*c.key.CreditLimit)
	}
	writeJSON(w, map[string]any{"data": map[string]any{
		"name": c.key.Name, "label": c.key.Prefix, "usage": store.ToUSD(c.key.Usage),
		"limit": limit, "balance": store.ToUSD(c.user.Balance),
	}})
}
