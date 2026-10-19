package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/local4a/plexrouter/internal/provider"
	"github.com/local4a/plexrouter/internal/store"
)

// maxAttempts bounds how many upstream calls one request may make while
// falling back across keys and endpoints.
const maxAttempts = 8

// route is one attempt: a model endpoint served through one provider key.
type route struct {
	cand     *candidate
	provider *store.Provider
	key      *store.ProviderKey // nil for providers that need no key
	adapter  provider.Adapter
	started  time.Time
}

// needsKey reports whether a provider type can't work without an API key.
// OpenAI-compatible servers (Ollama, vLLM…) often run without one.
func needsKey(typ string) bool { return typ != provider.TypeOpenAICompatible }

// targets returns a provider and its usable keys, rotated round-robin so load
// spreads across keys. A provider with no keys that doesn't need one yields a
// single nil key.
func (g *Gateway) targets(ctx context.Context, providerID int64) (*store.Provider, []*store.ProviderKey, error) {
	p, err := g.store.GetProvider(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	keys, err := g.store.ListProviderKeys(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	var usable []*store.ProviderKey
	for i := range keys {
		if _, cooling := g.health.keyCoolingUntil(keys[i].ID); keys[i].Usable() && !cooling {
			usable = append(usable, &keys[i])
		}
	}
	if len(usable) == 0 {
		if len(keys) == 0 && !needsKey(p.Type) {
			return p, []*store.ProviderKey{nil}, nil
		}
		return nil, nil, &apiError{http.StatusServiceUnavailable, "no_provider_keys",
			fmt.Sprintf("provider %q has no usable API key", p.Name)}
	}
	off := g.health.rotate(p.ID, len(usable))
	return p, append(usable[off:], usable[:off]...), nil
}

func (g *Gateway) adapterWith(p *store.Provider, k *store.ProviderKey) (provider.Adapter, error) {
	apiKey := ""
	if k != nil {
		var err error
		if apiKey, err = g.box.Decrypt(k.KeyEnc); err != nil {
			return nil, err
		}
	}
	return provider.New(provider.Config{Type: p.Type, BaseURL: p.BaseURL, APIKey: apiKey}, g.client)
}

// ProviderAdapter builds an adapter for admin operations (connection test,
// listing upstream models) using the provider's next usable key.
func (g *Gateway) ProviderAdapter(ctx context.Context, providerID int64) (provider.Adapter, error) {
	p, keys, err := g.targets(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return g.adapterWith(p, keys[0])
}

// KeyCooldownUntil reports a provider key's temporary cooldown, if any.
func (g *Gateway) KeyCooldownUntil(id int64) *time.Time {
	if t, ok := g.health.keyCoolingUntil(id); ok {
		return &t
	}
	return nil
}

// ClearKeyCooldown lets an admin put a key back into rotation immediately.
func (g *Gateway) ClearKeyCooldown(id int64) { g.health.clearKey(id) }

// EndpointOpenUntil reports when an endpoint's open circuit closes, if open.
func (g *Gateway) EndpointOpenUntil(id int64) *time.Time {
	if t, ok := g.health.endpointOpenUntil(id); ok {
		return &t
	}
	return nil
}

type failure int

const (
	failEndpoint failure = iota // upstream/network problem: try the next endpoint
	failKeyAuth                 // key rejected: disable it, try another key
	failKeyQuota                // key out of credit: disable it, try another key
	failKeyRate                 // key rate-limited: cool it down, try another key
	failClient                  // the request itself is bad: stop and report
)

// classify decides what an upstream error says about the key and endpoint.
func classify(err error) failure {
	var pe *provider.Error
	if !errors.As(err, &pe) {
		return failEndpoint
	}
	msg := strings.ToLower(pe.Message + " " + pe.Code)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(msg, s) {
				return true
			}
		}
		return false
	}
	switch {
	case pe.Status == http.StatusPaymentRequired,
		has("insufficient_quota", "billing", "credit balance", "insufficient balance", "余额不足"):
		return failKeyQuota
	case pe.Status == http.StatusUnauthorized,
		pe.Status == http.StatusForbidden && has("key", "auth"),
		pe.Status == http.StatusBadRequest && has("api key not valid", "api_key_invalid", "invalid api key", "invalid x-api-key"):
		return failKeyAuth
	case pe.Status == http.StatusTooManyRequests:
		return failKeyRate
	case pe.Status == http.StatusBadRequest, pe.Status == http.StatusRequestEntityTooLarge, pe.Status == http.StatusUnprocessableEntity:
		return failClient
	}
	return failEndpoint
}

// dispatch tries candidates in order, and each candidate's provider keys in
// rotation, until do succeeds. do performs one upstream call and, on
// success, writes the response and records usage itself. Failed attempts
// are logged here and update key and endpoint health.
func (g *Gateway) dispatch(ctx context.Context, c *caller, cands []candidate, stream bool, do func(*route) error) error {
	cands = g.health.order(cands)
	var lastErr error
	attempts := 0
	for i := range cands {
		cand := &cands[i]
		if err := c.checkFunds(cand.model); err != nil {
			lastErr = err
			continue
		}
		p, keys, err := g.targets(ctx, cand.endpoint.ProviderID)
		if err != nil {
			lastErr = err
			continue
		}
	keys:
		for _, k := range keys {
			if attempts == maxAttempts {
				return lastErr
			}
			attempts++
			ad, err := g.adapterWith(p, k)
			if err != nil {
				lastErr = err
				g.log.Error("provider key unusable", "provider", p.Name, "err", err)
				continue
			}
			rt := &route{cand: cand, provider: p, key: k, adapter: ad, started: time.Now()}
			err = do(rt)
			if err == nil {
				g.health.endpointOK(cand.endpoint.ID)
				return nil
			}
			lastErr = err
			if errors.Is(err, provider.ErrUnsupported) {
				break // this provider type can't do it; try the next endpoint
			}
			g.record(c, rt, store.TokenUsage{}, 0, stream, err)
			if ctx.Err() != nil {
				return lastErr
			}
			kind := classify(err)
			if k == nil && kind != failClient {
				kind = failEndpoint
			}
			switch kind {
			case failKeyAuth:
				g.failKey(p, k, store.KeyAuthFailed, err)
			case failKeyQuota:
				g.failKey(p, k, store.KeyQuotaExceeded, err)
			case failKeyRate:
				var pe *provider.Error
				errors.As(err, &pe)
				g.health.coolKey(k.ID, pe.RetryAfter)
				g.store.NoteProviderKeyError(context.Background(), k.ID, err.Error())
			case failClient:
				return lastErr
			default:
				g.health.endpointFailed(cand.endpoint.ID)
				break keys
			}
		}
	}
	if lastErr == nil {
		lastErr = &apiError{http.StatusServiceUnavailable, "no_endpoints", "no provider is currently available for this model"}
	}
	return lastErr
}

func (g *Gateway) failKey(p *store.Provider, k *store.ProviderKey, status string, err error) {
	g.log.Warn("provider key taken out of rotation", "provider", p.Name, "key", k.Name, "status", status, "err", err)
	if e := g.store.FailProviderKey(context.Background(), k.ID, status, err.Error()); e != nil {
		g.log.Error("mark provider key failed", "err", e)
	}
}

// checkLimits applies the key's and the user's requests-per-minute limits.
func (g *Gateway) checkLimits(c *caller) error {
	var limits []limit
	if c.key.RateLimitRPM != nil && *c.key.RateLimitRPM > 0 {
		limits = append(limits, limit{fmt.Sprintf("k%d", c.key.ID), *c.key.RateLimitRPM})
	}
	userRPM := g.options().DefaultUserRPM
	if c.user.RateLimitRPM != nil {
		userRPM = *c.user.RateLimitRPM
	}
	if userRPM > 0 {
		limits = append(limits, limit{fmt.Sprintf("u%d", c.user.ID), userRPM})
	}
	if len(limits) == 0 {
		return nil
	}
	if ok, wait := g.limiter.allow(limits...); !ok {
		return &rateLimited{wait}
	}
	return nil
}

type rateLimited struct{ wait time.Duration }

func (e *rateLimited) Error() string {
	return fmt.Sprintf("rate limit exceeded; retry in %ds", int(e.wait.Seconds())+1)
}

// allowedSlugs drops models the key may not call; it fails if none remain.
func (c *caller) allowedSlugs(slugs []string) ([]string, error) {
	var out []string
	requested := false
	for _, s := range slugs {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		requested = true
		if c.key.AllowsModel(s) {
			out = append(out, s)
		}
	}
	if requested && len(out) == 0 {
		return nil, &apiError{http.StatusForbidden, "model_not_allowed", "this API key is not allowed to use the requested model"}
	}
	return out, nil
}
