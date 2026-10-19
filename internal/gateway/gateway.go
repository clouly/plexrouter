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
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/clouly/plexrouter/internal/provider"
	"github.com/clouly/plexrouter/internal/secure"
	"github.com/clouly/plexrouter/internal/store"
)

const (
	KeyPrefix    = "sk-plex-"
	maxBodyBytes = 50 << 20 // requests may carry base64 images
)

type Gateway struct {
	store   *store.Store
	box     *secure.Box
	client  *http.Client
	timeout time.Duration
	log     *slog.Logger
}

func New(st *store.Store, box *secure.Box, timeout time.Duration, log *slog.Logger) *Gateway {
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
	return &Gateway{store: st, box: box, client: &http.Client{Transport: transport}, timeout: timeout, log: log}
}

// Register mounts the API under /v1 and /api/v1 (OpenRouter-style base URL).
func (g *Gateway) Register(mux *http.ServeMux) {
	for _, p := range []string{"/v1", "/api/v1"} {
		mux.HandleFunc("POST "+p+"/chat/completions", g.handleChat)
		mux.HandleFunc("POST "+p+"/embeddings", g.handleEmbeddings)
		mux.HandleFunc("GET "+p+"/models", g.handleModels)
		mux.HandleFunc("GET "+p+"/key", g.handleKey)
		mux.HandleFunc(p+"/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "unknown endpoint "+r.Method+" "+r.URL.Path)
		})
	}
}

// Adapter builds a provider adapter from a stored provider.
func (g *Gateway) Adapter(p *store.Provider) (provider.Adapter, error) {
	key, err := g.box.Decrypt(p.APIKeyEnc)
	if err != nil {
		return nil, err
	}
	return provider.New(provider.Config{Type: p.Type, BaseURL: p.BaseURL, APIKey: key}, g.client)
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

func writeAPIError(w http.ResponseWriter, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.message)
		return
	}
	var pe *provider.Error
	if errors.As(err, &pe) {
		status := pe.Status
		if status == 0 || status < 400 || status == http.StatusUnauthorized || status == http.StatusForbidden {
			// Network failures and upstream auth problems are the gateway's
			// problem, not the client's.
			status = http.StatusBadGateway
		}
		writeError(w, status, "upstream_error", pe.Error())
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "timeout", "upstream request timed out")
		return
	}
	writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// --- auth & routing ---

type caller struct {
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

func (g *Gateway) adapterFor(ctx context.Context, providerID int64) (provider.Adapter, *store.Provider, error) {
	p, err := g.store.GetProvider(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	a, err := g.Adapter(p)
	return a, p, err
}

// --- billing ---

// cost converts token counts to nano-USD. Prices are USD per 1M tokens, so one
// token costs price*1000 nano-USD.
func cost(m *store.Model, prompt, completion int) int64 {
	return int64(math.Round(float64(prompt)*m.InputPrice*1000 + float64(completion)*m.OutputPrice*1000))
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

func (g *Gateway) record(c *caller, cand *candidate, providerName string, prompt, completion int, nano int64,
	started time.Time, stream bool, callErr error) {
	l := store.RequestLog{
		UserID: c.user.ID, APIKeyID: c.key.ID, APIKeyName: c.key.Name,
		PromptTokens: prompt, CompletionTokens: completion, Cost: nano,
		LatencyMS: time.Since(started).Milliseconds(), Status: "success", Stream: stream,
	}
	if cand != nil {
		l.Model, l.ProviderID, l.ProviderName = cand.model.Slug, cand.endpoint.ProviderID, providerName
	}
	if callErr != nil {
		l.Status, l.Error = "error", callErr.Error()
		if len(l.Error) > 1000 {
			l.Error = l.Error[:1000]
		}
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
	cands, err := g.candidates(r.Context(), append([]string{req.Model}, req.Models...))
	if err != nil {
		writeAPIError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)
	defer cancel()
	promptEstimate := estimatePrompt(req)

	var lastErr error
	for i := range cands {
		cand := &cands[i]
		if err := c.checkFunds(cand.model); err != nil {
			lastErr = err
			continue
		}
		adapter, prov, err := g.adapterFor(ctx, cand.endpoint.ProviderID)
		if err != nil {
			lastErr = err
			g.log.Warn("provider unusable", "provider", cand.endpoint.ProviderID, "err", err)
			continue
		}
		started := time.Now()
		if req.Stream {
			stream, err := adapter.ChatStream(ctx, req, cand.endpoint.UpstreamModel)
			if err != nil {
				lastErr = err
				g.record(c, cand, prov.Name, 0, 0, 0, started, true, err)
				if !provider.Retryable(err) || ctx.Err() != nil {
					break
				}
				continue
			}
			g.relayStream(w, c, cand, prov.Name, stream, started, promptEstimate)
			return
		}
		resp, err := adapter.Chat(ctx, req, cand.endpoint.UpstreamModel)
		if err != nil {
			lastErr = err
			g.record(c, cand, prov.Name, 0, 0, 0, started, false, err)
			if !provider.Retryable(err) || ctx.Err() != nil {
				break
			}
			continue
		}
		prompt, completion := promptEstimate, 0
		if u := provider.UsageOf(resp); u != nil {
			prompt, completion = u.PromptTokens, u.CompletionTokens
		} else {
			completion = estimateTokens(len(contentOf(resp)))
		}
		nano := cost(cand.model, prompt, completion)
		resp["model"] = cand.model.Slug
		resp["provider"] = prov.Name
		setUsageCost(resp, nano)
		writeJSON(w, resp)
		g.record(c, cand, prov.Name, prompt, completion, nano, started, false, nil)
		return
	}
	writeAPIError(w, lastErr)
}

// relayStream copies chunks to the client as server-sent events. Once the
// first byte is written there is no falling back, so errors are reported
// in-band as an "error" event.
func (g *Gateway) relayStream(w http.ResponseWriter, c *caller, cand *candidate, providerName string,
	stream provider.Stream, started time.Time, promptEstimate int) {
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
			setUsageCost(ch, cost(cand.model, u.PromptTokens, u.CompletionTokens))
		}
		ch["model"] = cand.model.Slug
		ch["provider"] = providerName
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

	prompt, completion := promptEstimate, estimateTokens(outBytes)
	if usage != nil {
		prompt, completion = usage.PromptTokens, usage.CompletionTokens
	}
	g.record(c, cand, providerName, prompt, completion, cost(cand.model, prompt, completion), started, true, streamErr)
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
	var body provider.Object
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	slug, _ := body["model"].(string)
	cands, err := g.candidates(r.Context(), []string{slug})
	if err != nil {
		writeAPIError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)
	defer cancel()

	var lastErr error
	for i := range cands {
		cand := &cands[i]
		if err := c.checkFunds(cand.model); err != nil {
			lastErr = err
			continue
		}
		adapter, prov, err := g.adapterFor(ctx, cand.endpoint.ProviderID)
		if err != nil {
			lastErr = err
			continue
		}
		started := time.Now()
		resp, err := adapter.Embeddings(ctx, body, cand.endpoint.UpstreamModel)
		if err != nil {
			lastErr = err
			if !errors.Is(err, provider.ErrUnsupported) {
				g.record(c, cand, prov.Name, 0, 0, 0, started, false, err)
			}
			if !provider.Retryable(err) || ctx.Err() != nil {
				break
			}
			continue
		}
		prompt := 0
		if u := provider.UsageOf(resp); u != nil {
			prompt = u.PromptTokens
		}
		nano := cost(cand.model, prompt, 0)
		resp["model"] = cand.model.Slug
		resp["provider"] = prov.Name
		setUsageCost(resp, nano)
		writeJSON(w, resp)
		g.record(c, cand, prov.Name, prompt, 0, nano, started, false, nil)
		return
	}
	writeAPIError(w, lastErr)
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
		data = append(data, map[string]any{
			"id": m.Slug, "object": "model", "created": m.CreatedAt.Unix(), "owned_by": owner,
			"name": m.Name, "description": m.Description, "context_length": m.ContextLength,
			"pricing": map[string]string{
				"prompt":     perToken(m.InputPrice),
				"completion": perToken(m.OutputPrice),
			},
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
