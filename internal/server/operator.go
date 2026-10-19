package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local4a/plexrouter/internal/catalog"
	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/store"
)

// Operator-facing admin features: provider key pools, health, profit
// reporting, alerts and importing models from the public catalog.

const healthWindow = 24 * time.Hour

type healthJSON struct {
	Requests     int64    `json:"requests_24h"`
	SuccessRate  *float64 `json:"success_rate_24h"`
	AvgLatencyMS *float64 `json:"avg_latency_ms_24h"`
	Status       string   `json:"status"`
}

func healthStatus(h store.Health, usableKeys int) string {
	if usableKeys == 0 {
		return "no_keys"
	}
	rate := h.SuccessRate()
	switch {
	case h.Requests < 10 || rate == nil:
		return "idle"
	case *rate >= 0.95:
		return "healthy"
	case *rate >= 0.80:
		return "degraded"
	}
	return "down"
}

type providerJSON struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	BaseURL    string     `json:"base_url"`
	Enabled    bool       `json:"enabled"`
	KeysTotal  int        `json:"keys_total"`
	KeysActive int        `json:"keys_active"`
	Health     healthJSON `json:"health"`
	CreatedAt  time.Time  `json:"created_at"`
}

type providerKeyJSON struct {
	ID            int64      `json:"id"`
	ProviderID    int64      `json:"provider_id"`
	Name          string     `json:"name"`
	KeyHint       string     `json:"key_hint"`
	Enabled       bool       `json:"enabled"`
	Status        string     `json:"status"`
	LastError     string     `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`
	CooldownUntil *time.Time `json:"cooldown_until"`
	LastUsedAt    *time.Time `json:"last_used_at"`
	Requests      int64      `json:"requests_24h"`
	Errors        int64      `json:"errors_24h"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (s *Server) providerKeyDTO(k *store.ProviderKey, h store.Health) providerKeyJSON {
	hint := "(unreadable)"
	if key, err := s.box.Decrypt(k.KeyEnc); err == nil {
		hint = secure.Mask(key)
	}
	j := providerKeyJSON{ID: k.ID, ProviderID: k.ProviderID, Name: k.Name, KeyHint: hint, Enabled: k.Enabled,
		Status: k.Status, LastError: k.LastError, LastErrorAt: k.LastErrorAt, LastUsedAt: k.LastUsedAt,
		Requests: h.Requests, Errors: h.Requests - h.Successes, CreatedAt: k.CreatedAt}
	j.CooldownUntil = s.gw.KeyCooldownUntil(k.ID)
	switch {
	case !k.Enabled:
		j.Status = "disabled"
	case k.Status == store.KeyActive && j.CooldownUntil != nil:
		j.Status = "cooling_down"
	}
	return j
}

// usableKeys counts keys in rotation; keyless OpenAI-compatible providers
// count as one so they don't show up as broken.
func (s *Server) usableKeys(p *store.Provider, keys []store.ProviderKey) int {
	n, total := 0, 0
	for i := range keys {
		if keys[i].ProviderID != p.ID {
			continue
		}
		total++
		if keys[i].Usable() && s.gw.KeyCooldownUntil(keys[i].ID) == nil {
			n++
		}
	}
	if total == 0 && p.Type == "openai_compatible" {
		return 1
	}
	return n
}

// providerViews returns providers with key counts and 24 h health.
func (s *Server) providerViews(ctx context.Context) ([]providerJSON, error) {
	ps, err := s.store.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := s.store.ListProviderKeys(ctx, 0)
	if err != nil {
		return nil, err
	}
	health, err := s.store.ProviderHealth(ctx, time.Now().Add(-healthWindow))
	if err != nil {
		return nil, err
	}
	out := make([]providerJSON, 0, len(ps))
	for i := range ps {
		p := &ps[i]
		total := 0
		for _, k := range keys {
			if k.ProviderID == p.ID {
				total++
			}
		}
		usable := s.usableKeys(p, keys)
		h := health[p.ID]
		active := usable
		if total == 0 {
			active = 0
		}
		out = append(out, providerJSON{ID: p.ID, Name: p.Name, Type: p.Type, BaseURL: p.BaseURL, Enabled: p.Enabled,
			KeysTotal: total, KeysActive: active, CreatedAt: p.CreatedAt,
			Health: healthJSON{Requests: h.Requests, SuccessRate: h.SuccessRate(), AvgLatencyMS: h.AvgLatencyMS,
				Status: healthStatus(h, usable)}})
	}
	return out, nil
}

func (s *Server) providerView(ctx context.Context, id int64) (*providerJSON, error) {
	views, err := s.providerViews(ctx)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].ID == id {
			return &views[i], nil
		}
	}
	return nil, store.ErrNotFound
}

// --- provider keys ---

func (s *Server) handleListProviderKeys(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if _, err := s.store.GetProvider(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	keys, err := s.store.ListProviderKeys(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	health, err := s.store.ProviderKeyHealth(r.Context(), time.Now().Add(-healthWindow))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]providerKeyJSON, 0, len(keys))
	for i := range keys {
		out = append(out, s.providerKeyDTO(&keys[i], health[keys[i].ID]))
	}
	ok(w, map[string]any{"data": out})
}

// addProviderKeys encrypts and stores keys; with several, names are numbered.
func (s *Server) addProviderKeys(ctx context.Context, providerID int64, name string, raw []string) ([]store.ProviderKey, error) {
	var keys []string
	for _, k := range raw {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		name = "Key"
	}
	names, encs := make([]string, len(keys)), make([]string, len(keys))
	for i, k := range keys {
		names[i] = name
		if len(keys) > 1 {
			names[i] = fmt.Sprintf("%s #%d", name, i+1)
		}
		encs[i] = s.box.Encrypt(k)
	}
	return s.store.AddProviderKeys(ctx, providerID, names, encs)
}

func (s *Server) handleCreateProviderKeys(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in struct {
		APIKeys []string `json:"api_keys"`
		Name    string   `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	keys, err := s.addProviderKeys(r.Context(), id, in.Name, in.APIKeys)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(keys) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "api_keys must contain at least one key")
		return
	}
	out := make([]providerKeyJSON, 0, len(keys))
	for i := range keys {
		out = append(out, s.providerKeyDTO(&keys[i], store.Health{}))
	}
	ok(w, map[string]any{"data": out})
}

func keyPathIDs(w http.ResponseWriter, r *http.Request) (providerID, keyID int64, valid bool) {
	providerID, valid = pathID(w, r)
	if !valid {
		return
	}
	keyID, err := strconv.ParseInt(r.PathValue("keyId"), 10, 64)
	if err != nil || keyID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "invalid key id")
		return 0, 0, false
	}
	return providerID, keyID, true
}

func (s *Server) handleUpdateProviderKey(w http.ResponseWriter, r *http.Request) {
	pid, kid, valid := keyPathIDs(w, r)
	if !valid {
		return
	}
	var in struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	k, err := s.store.UpdateProviderKey(r.Context(), pid, kid, store.ProviderKeyUpdate{Name: in.Name, Enabled: in.Enabled})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Enabled != nil && *in.Enabled {
		s.gw.ClearKeyCooldown(kid)
	}
	ok(w, map[string]any{"key": s.providerKeyDTO(k, store.Health{})})
}

func (s *Server) handleDeleteProviderKey(w http.ResponseWriter, r *http.Request) {
	pid, kid, valid := keyPathIDs(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteProviderKey(r.Context(), pid, kid); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"ok": true})
}

// --- reports ---

type reportRowJSON struct {
	Requests            int64   `json:"requests"`
	Tokens              int64   `json:"tokens"`
	Revenue             float64 `json:"revenue"`
	UpstreamCost        float64 `json:"upstream_cost"`
	Profit              float64 `json:"profit"`
	UnknownCostRequests int64   `json:"unknown_cost_requests"`
}

func reportRowDTO(r store.ReportRow) reportRowJSON {
	return reportRowJSON{r.Requests, r.Tokens, store.ToUSD(r.Revenue), store.ToUSD(r.UpstreamCost),
		store.ToUSD(r.Profit), r.UnknownCostRequests}
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 30
	}
	days = min(days, 365)
	rep, err := s.store.Report(r.Context(), days)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type daily struct {
		Date string `json:"date"`
		reportRowJSON
	}
	type byModel struct {
		Model string `json:"model"`
		reportRowJSON
	}
	type byProvider struct {
		ProviderID  int64    `json:"provider_id"`
		Provider    string   `json:"provider"`
		SuccessRate *float64 `json:"success_rate"`
		reportRowJSON
	}
	out := map[string]any{"totals": reportRowDTO(rep.Totals)}
	d := make([]daily, 0, len(rep.Daily))
	for _, x := range rep.Daily {
		d = append(d, daily{x.Date, reportRowDTO(x.ReportRow)})
	}
	m := make([]byModel, 0, len(rep.ByModel))
	for _, x := range rep.ByModel {
		m = append(m, byModel{x.Model, reportRowDTO(x.ReportRow)})
	}
	p := make([]byProvider, 0, len(rep.ByProvider))
	for _, x := range rep.ByProvider {
		p = append(p, byProvider{x.ProviderID, x.Provider, x.SuccessRate, reportRowDTO(x.ReportRow)})
	}
	out["daily"], out["by_model"], out["by_provider"] = d, m, p
	ok(w, out)
}

// --- alerts ---

type alertJSON struct {
	Level      string         `json:"level"`
	Kind       string         `json:"kind"`
	Message    string         `json:"message"` // English fallback
	Params     map[string]any `json:"params"`  // for the console to localize the message
	ProviderID int64          `json:"provider_id,omitempty"`
	ModelID    int64          `json:"model_id,omitempty"`
	KeyID      int64          `json:"key_id,omitempty"`
}

// alerts lists things an operator should fix, most severe first.
func (s *Server) alerts(ctx context.Context) ([]alertJSON, error) {
	views, err := s.providerViews(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := s.store.ListProviderKeys(ctx, 0)
	if err != nil {
		return nil, err
	}
	models, err := s.store.ListModels(ctx, false)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	enabled := map[int64]bool{}
	for _, v := range views {
		names[v.ID], enabled[v.ID] = v.Name, v.Enabled
	}
	var errs, warns []alertJSON
	for _, v := range views {
		if !v.Enabled {
			continue
		}
		switch v.Health.Status {
		case "no_keys":
			errs = append(errs, alertJSON{Level: "error", Kind: "provider_no_keys", ProviderID: v.ID,
				Message: fmt.Sprintf("Provider %q has no usable API key", v.Name), Params: map[string]any{"provider": v.Name}})
		case "down":
			rate := math.Round(*v.Health.SuccessRate * 100)
			errs = append(errs, alertJSON{Level: "error", Kind: "provider_down", ProviderID: v.ID,
				Message: fmt.Sprintf("Provider %q is failing: %.0f%% of requests succeeded in the last 24 h", v.Name, rate),
				Params:  map[string]any{"provider": v.Name, "rate": rate}})
		}
	}
	for _, k := range keys {
		if k.Enabled && k.Status != store.KeyActive && enabled[k.ProviderID] {
			reason, why := "auth", "was rejected (authentication failed)"
			if k.Status == store.KeyQuotaExceeded {
				reason, why = "quota", "ran out of credit"
			}
			warns = append(warns, alertJSON{Level: "warning", Kind: "key_failed", ProviderID: k.ProviderID, KeyID: k.ID,
				Message: fmt.Sprintf("Key %q of %q %s and was switched off", k.Name, names[k.ProviderID], why),
				Params:  map[string]any{"provider": names[k.ProviderID], "key": k.Name, "reason": reason}})
		}
	}
	missingCost := 0
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		routable, costed := false, false
		for _, e := range m.Endpoints {
			if e.Enabled && enabled[e.ProviderID] {
				routable = true
				costed = costed || e.InputCost != nil || e.OutputCost != nil
			}
		}
		if !routable {
			errs = append(errs, alertJSON{Level: "error", Kind: "model_unroutable", ModelID: m.ID,
				Message: fmt.Sprintf("Model %q is enabled but has no enabled endpoint", m.Slug), Params: map[string]any{"model": m.Slug}})
		} else if !costed && (m.InputPrice > 0 || m.OutputPrice > 0) {
			missingCost++
		}
	}
	if missingCost > 0 {
		warns = append(warns, alertJSON{Level: "warning", Kind: "missing_cost",
			Message: fmt.Sprintf("%d enabled paid models have no upstream cost set, so their profit is unknown", missingCost),
			Params:  map[string]any{"n": missingCost}})
	}
	return append(errs, warns...), nil
}

// --- catalog ---

// SetCatalogURL points catalog imports at another source (used in tests).
func (s *Server) SetCatalogURL(url string) { s.catalog.URL = url }

type catalogModelJSON struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	ContextLength int      `json:"context_length"`
	InputPrice    float64  `json:"input_price"`
	OutputPrice   float64  `json:"output_price"`
	CacheRead     *float64 `json:"cache_read_price"`
	CacheWrite    *float64 `json:"cache_write_price"`
	UpstreamMatch *string  `json:"upstream_match"`
	Exists        bool     `json:"exists"`
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	models, fetched, err := s.catalog.Models(ctx)
	if err != nil {
		s.log.Warn("catalog fetch failed", "err", err)
		writeErr(w, http.StatusBadGateway, "catalog_unavailable", "could not fetch the model catalog: "+err.Error())
		return
	}
	var upstream []string
	if pid, _ := strconv.ParseInt(r.URL.Query().Get("provider_id"), 10, 64); pid > 0 {
		a, err := s.gw.ProviderAdapter(ctx, pid)
		if err == nil {
			upstream, err = a.ListModels(ctx)
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, "upstream_error", "could not list the provider's models: "+err.Error())
			return
		}
	}
	existing, err := s.store.ListModels(r.Context(), false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	have := map[string]bool{}
	for _, m := range existing {
		have[m.Slug] = true
	}
	out := make([]catalogModelJSON, 0, len(models))
	for _, m := range models {
		j := catalogModelJSON{ID: m.ID, Name: m.Name, Description: m.Description, ContextLength: m.ContextLength,
			InputPrice: m.InputPrice, OutputPrice: m.OutputPrice, CacheRead: m.CacheReadPrice, CacheWrite: m.CacheWritePrice,
			Exists: have[m.ID]}
		if upstream != nil {
			if match := catalog.Match(m.ID, upstream); match != "" {
				j.UpstreamMatch = &match
			}
		}
		out = append(out, j)
	}
	ok(w, map[string]any{"data": out, "fetched_at": fetched.UTC()})
}

// markup applies a percentage to a reference price, rounded to 1e-6 USD/1M.
func markup(price, percent float64) float64 {
	return math.Round(price*(1+percent/100)*1e6) / 1e6
}

func markupPtr(price *float64, percent float64) *float64 {
	if price == nil {
		return nil
	}
	v := markup(*price, percent)
	return &v
}

func (s *Server) handleCatalogImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProviderID int64 `json:"provider_id"`
		Items      []struct {
			ID            string `json:"id"`
			UpstreamModel string `json:"upstream_model"`
		} `json:"items"`
		MarkupPercent  float64 `json:"markup_percent"`
		Enable         bool    `json:"enable"`
		UpdateExisting bool    `json:"update_existing"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.MarkupPercent < -100 || in.MarkupPercent > 10000 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "markup_percent must be between -100 and 10000")
		return
	}
	ctx := r.Context()
	if _, err := s.store.GetProvider(ctx, in.ProviderID); err != nil {
		s.fail(w, r, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	models, _, err := s.catalog.Models(cctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "catalog_unavailable", "could not fetch the model catalog: "+err.Error())
		return
	}
	ref := map[string]catalog.Model{}
	for _, m := range models {
		ref[m.ID] = m
	}

	var created, updated, added int
	for _, item := range in.Items {
		m, found := ref[item.ID]
		upstream := strings.TrimSpace(item.UpstreamModel)
		if !found || upstream == "" {
			continue
		}
		inCost, outCost := m.InputPrice, m.OutputPrice
		ep := store.Endpoint{ProviderID: in.ProviderID, UpstreamModel: upstream, Enabled: true,
			InputCost: &inCost, OutputCost: &outCost, CacheReadCost: m.CacheReadPrice, CacheWriteCost: m.CacheWritePrice}
		sellIn, sellOut := markup(m.InputPrice, in.MarkupPercent), markup(m.OutputPrice, in.MarkupPercent)
		sellRead, sellWrite := markupPtr(m.CacheReadPrice, in.MarkupPercent), markupPtr(m.CacheWritePrice, in.MarkupPercent)

		existing, err := s.store.GetModelBySlug(ctx, m.ID)
		if errors.Is(err, store.ErrNotFound) {
			if _, err := s.store.CreateModel(ctx, store.Model{Slug: m.ID, Name: m.Name, Description: m.Description,
				ContextLength: m.ContextLength, InputPrice: sellIn, OutputPrice: sellOut,
				CacheReadPrice: sellRead, CacheWritePrice: sellWrite, Enabled: in.Enable,
				Endpoints: []store.Endpoint{ep}}); err != nil {
				s.fail(w, r, err)
				return
			}
			created++
			continue
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		hasEndpoint := false
		for _, e := range existing.Endpoints {
			hasEndpoint = hasEndpoint || (e.ProviderID == in.ProviderID && e.UpstreamModel == upstream)
		}
		if !hasEndpoint {
			ep.Priority = len(existing.Endpoints)
			if err := s.store.AddEndpoint(ctx, existing.ID, ep); err != nil {
				s.fail(w, r, err)
				return
			}
			added++
		}
		if in.UpdateExisting {
			upd := store.ModelUpdate{Name: &m.Name, Description: &m.Description, ContextLength: &m.ContextLength,
				InputPrice: &sellIn, OutputPrice: &sellOut, CacheReadPrice: &sellRead, CacheWritePrice: &sellWrite}
			if in.Enable {
				upd.Enabled = &in.Enable
			}
			if _, err := s.store.UpdateModel(ctx, existing.ID, upd); err != nil {
				s.fail(w, r, err)
				return
			}
			updated++
		}
	}
	ok(w, map[string]any{"created": created, "updated": updated, "endpoints_added": added})
}
