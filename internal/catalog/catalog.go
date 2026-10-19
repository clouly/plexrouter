// Package catalog fetches OpenRouter's public model list, used as a reference
// (names, context lengths, prices) when an operator imports models.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultURL = "https://openrouter.ai/api/v1/models"
	cacheTTL   = 10 * time.Minute
)

type Model struct {
	ID              string // e.g. "openai/gpt-4o"
	Name            string
	Description     string
	ContextLength   int
	InputPrice      float64 // USD per 1M tokens
	OutputPrice     float64
	CacheReadPrice  *float64 // nil when the catalog lists none
	CacheWritePrice *float64
}

// Catalog caches the model list.
type Catalog struct {
	URL    string
	Client *http.Client

	mu      sync.Mutex
	models  []Model
	fetched time.Time
}

func New() *Catalog {
	return &Catalog{URL: DefaultURL, Client: &http.Client{Timeout: 30 * time.Second}}
}

// Models returns the cached list, refreshing it when stale.
func (c *Catalog) Models(ctx context.Context) ([]Model, time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.models != nil && time.Since(c.fetched) < cacheTTL {
		return c.models, c.fetched, nil
	}
	models, err := c.fetch(ctx)
	if err != nil {
		if c.models != nil { // serve stale data rather than nothing
			return c.models, c.fetched, nil
		}
		return nil, time.Time{}, err
	}
	c.models, c.fetched = models, time.Now()
	return c.models, c.fetched, nil
}

func (c *Catalog) fetch(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt          string `json:"prompt"`
				Completion      string `json:"completion"`
				InputCacheRead  string `json:"input_cache_read"`
				InputCacheWrite string `json:"input_cache_write"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	out := make([]Model, 0, len(body.Data))
	for _, d := range body.Data {
		in, err1 := strconv.ParseFloat(d.Pricing.Prompt, 64)
		outP, err2 := strconv.ParseFloat(d.Pricing.Completion, 64)
		// Routers and variable-priced entries use negative or missing prices.
		if d.ID == "" || err1 != nil || err2 != nil || in < 0 || outP < 0 {
			continue
		}
		out = append(out, Model{
			ID: d.ID, Name: d.Name, Description: d.Description, ContextLength: d.ContextLength,
			InputPrice: round(in * 1e6), OutputPrice: round(outP * 1e6),
			CacheReadPrice: perMillion(d.Pricing.InputCacheRead), CacheWritePrice: perMillion(d.Pricing.InputCacheWrite),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// perMillion converts an optional per-token price string to USD per 1M.
func perMillion(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if s == "" || err != nil || v < 0 {
		return nil
	}
	r := round(v * 1e6)
	return &r
}

// round trims float noise from per-token → per-million conversion.
func round(v float64) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 10, 64), 64)
	return r
}

var datedSuffix = regexp.MustCompile(`-(\d{8}|\d{4}-\d{2}-\d{2})$`)

func normalize(id string) string {
	return strings.ReplaceAll(strings.ToLower(id), ".", "-")
}

// Match finds the upstream model that serves a catalog model, comparing names
// without the vendor prefix and treating "." and "-" alike (OpenRouter's
// "anthropic/claude-sonnet-4.5" is Anthropic's "claude-sonnet-4-5"). An exact
// match wins; otherwise the newest dated snapshot ("…-20250929") is used.
func Match(catalogID string, upstream []string) string {
	want := normalize(catalogID)
	_, short, found := strings.Cut(want, "/")
	if !found {
		short = want
	}
	var dated []string
	for _, u := range upstream {
		n := normalize(u)
		if n == want || n == short {
			return u
		}
		if base := datedSuffix.ReplaceAllString(n, ""); base != n && (base == short || base == want) {
			dated = append(dated, u)
		}
	}
	if len(dated) == 0 {
		return ""
	}
	sort.Strings(dated)
	return dated[len(dated)-1]
}
