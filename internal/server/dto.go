package server

import (
	"time"

	"github.com/local4a/plexrouter/internal/store"
)

// JSON shapes of the console API (see docs/console-api.md). Money is USD.

type userJSON struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Balance      float64   `json:"balance"`
	Disabled     bool      `json:"disabled"`
	RateLimitRPM *int      `json:"rate_limit_rpm"`
	CreatedAt    time.Time `json:"created_at"`
}

func userDTO(u *store.User) userJSON {
	return userJSON{u.ID, u.Email, u.Name, u.Role, store.ToUSD(u.Balance), u.Disabled, u.RateLimitRPM, u.CreatedAt}
}

type keyJSON struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	CreditLimit   *float64   `json:"credit_limit"`
	Usage         float64    `json:"usage"`
	RateLimitRPM  *int       `json:"rate_limit_rpm"`
	AllowedModels []string   `json:"allowed_models"`
	Disabled      bool       `json:"disabled"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at"`
}

func keyDTO(k *store.APIKey) keyJSON {
	var limit *float64
	if k.CreditLimit != nil {
		v := store.ToUSD(*k.CreditLimit)
		limit = &v
	}
	return keyJSON{k.ID, k.Name, k.Prefix, limit, store.ToUSD(k.Usage), k.RateLimitRPM, k.AllowedModels, k.Disabled, k.CreatedAt, k.LastUsedAt}
}

type endpointJSON struct {
	ID               int64      `json:"id"`
	ProviderID       int64      `json:"provider_id"`
	ProviderName     string     `json:"provider_name"`
	UpstreamModel    string     `json:"upstream_model"`
	Priority         int        `json:"priority"`
	Enabled          bool       `json:"enabled"`
	InputCost        *float64   `json:"input_cost"`
	OutputCost       *float64   `json:"output_cost"`
	CircuitOpenUntil *time.Time `json:"circuit_open_until"`
}

type modelJSON struct {
	ID            int64          `json:"id"`
	Slug          string         `json:"slug"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	ContextLength int            `json:"context_length"`
	InputPrice    float64        `json:"input_price"`
	OutputPrice   float64        `json:"output_price"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     time.Time      `json:"created_at"`
	Endpoints     []endpointJSON `json:"endpoints"`
}

func (s *Server) modelDTO(m *store.Model) modelJSON {
	eps := make([]endpointJSON, 0, len(m.Endpoints))
	for _, e := range m.Endpoints {
		eps = append(eps, endpointJSON{e.ID, e.ProviderID, e.ProviderName, e.UpstreamModel, e.Priority, e.Enabled,
			e.InputCost, e.OutputCost, s.gw.EndpointOpenUntil(e.ID)})
	}
	return modelJSON{m.ID, m.Slug, m.Name, m.Description, m.ContextLength, m.InputPrice, m.OutputPrice, m.Enabled, m.CreatedAt, eps}
}

type publicModelJSON struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	ContextLength int     `json:"context_length"`
	InputPrice    float64 `json:"input_price"`
	OutputPrice   float64 `json:"output_price"`
	Providers     int     `json:"providers"`
}

type logJSON struct {
	ID               int64     `json:"id"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider"`
	APIKeyName       string    `json:"api_key_name"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	Cost             float64   `json:"cost"`
	LatencyMS        int64     `json:"latency_ms"`
	Status           string    `json:"status"`
	Error            string    `json:"error"`
	Stream           bool      `json:"stream"`
	CreatedAt        time.Time `json:"created_at"`
	UserEmail        string    `json:"user_email,omitempty"`
}

func logDTO(l *store.RequestLog) logJSON {
	return logJSON{l.ID, l.Model, l.ProviderName, l.APIKeyName, l.PromptTokens, l.CompletionTokens,
		store.ToUSD(l.Cost), l.LatencyMS, l.Status, l.Error, l.Stream, l.CreatedAt, ""}
}

// adminLogJSON adds what only operators may see.
type adminLogJSON struct {
	logJSON
	UpstreamCost *float64 `json:"upstream_cost"`
	ProviderKey  string   `json:"provider_key"`
}

func adminLogDTO(l *store.RequestLog) adminLogJSON {
	j := adminLogJSON{logJSON: logDTO(l), ProviderKey: l.ProviderKeyName}
	j.UserEmail = l.UserEmail
	if l.UpstreamCost != nil {
		v := store.ToUSD(*l.UpstreamCost)
		j.UpstreamCost = &v
	}
	return j
}

type dailyJSON struct {
	Date     string  `json:"date"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

func dailyDTO(ds []store.DailyStat) []dailyJSON {
	out := make([]dailyJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, dailyJSON{d.Date, d.Requests, d.Tokens, store.ToUSD(d.Cost)})
	}
	return out
}
