package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/clouly/plexrouter/internal/provider"
	"github.com/clouly/plexrouter/internal/secure"
	"github.com/clouly/plexrouter/internal/store"
)

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	users, err := s.store.CountUsers(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	models, providers, err := s.store.CountModels(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	balance, err := s.store.TotalBalance(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.store.Stats(ctx, 0, 14)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	today := st.Daily[len(st.Daily)-1]
	ok(w, map[string]any{
		"users": users, "providers": providers, "models": models, "total_balance": store.ToUSD(balance),
		"requests_today": today.Requests, "cost_today": store.ToUSD(today.Cost), "daily": dailyDTO(st.Daily),
	})
}

// --- providers ---

type providerInput struct {
	Name    *string `json:"name"`
	Type    *string `json:"type"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

func (in *providerInput) validate(creating bool) string {
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
	}
	if creating && (in.Name == nil || *in.Name == "") {
		return "name is required"
	}
	if in.Name != nil && *in.Name == "" {
		return "name must not be empty"
	}
	if creating && in.Type == nil {
		return "type is required"
	}
	if in.Type != nil && !slices.Contains(provider.Types, *in.Type) {
		return "type must be one of " + strings.Join(provider.Types, ", ")
	}
	if in.BaseURL != nil {
		*in.BaseURL = strings.TrimRight(strings.TrimSpace(*in.BaseURL), "/")
		if *in.BaseURL != "" && !strings.HasPrefix(*in.BaseURL, "http://") && !strings.HasPrefix(*in.BaseURL, "https://") {
			return "base_url must start with http:// or https://"
		}
	}
	return ""
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListProviders(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]providerJSON, 0, len(ps))
	for i := range ps {
		out = append(out, providerDTO(&ps[i], s.box))
	}
	ok(w, map[string]any{"data": out})
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	if msg := in.validate(true); msg != "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}
	p := store.Provider{Name: *in.Name, Type: *in.Type, Enabled: true}
	if in.BaseURL != nil {
		p.BaseURL = *in.BaseURL
	}
	if p.Type == provider.TypeOpenAICompatible && p.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "base_url is required for openai_compatible providers")
		return
	}
	if in.APIKey != nil {
		p.APIKeyEnc = s.box.Encrypt(strings.TrimSpace(*in.APIKey))
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	created, err := s.store.CreateProvider(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"provider": providerDTO(created, s.box)})
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	if msg := in.validate(false); msg != "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}
	upd := store.ProviderUpdate{Name: in.Name, Type: in.Type, BaseURL: in.BaseURL, Enabled: in.Enabled}
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		enc := s.box.Encrypt(strings.TrimSpace(*in.APIKey))
		upd.APIKeyEnc = &enc
	}
	p, err := s.store.UpdateProvider(r.Context(), id, upd)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"provider": providerDTO(p, s.box)})
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteProvider(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"ok": true})
}

func (s *Server) upstreamModels(r *http.Request) ([]string, time.Duration, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, 0, store.ErrNotFound
	}
	p, err := s.store.GetProvider(r.Context(), id)
	if err != nil {
		return nil, 0, err
	}
	a, err := s.gw.Adapter(p)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	start := time.Now()
	ids, err := a.ListModels(ctx)
	return ids, time.Since(start), err
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	ids, took, err := s.upstreamModels(r)
	if err == store.ErrNotFound {
		s.fail(w, r, err)
		return
	}
	res := map[string]any{"ok": err == nil, "latency_ms": took.Milliseconds()}
	if err != nil {
		res["message"] = err.Error()
	} else {
		res["message"] = fmt.Sprintf("Connected — %d models available", len(ids))
	}
	ok(w, res)
}

func (s *Server) handleUpstreamModels(w http.ResponseWriter, r *http.Request) {
	ids, _, err := s.upstreamModels(r)
	if err == store.ErrNotFound {
		s.fail(w, r, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	slices.Sort(ids)
	type item struct {
		ID string `json:"id"`
	}
	out := make([]item, 0, len(ids))
	for _, id := range slices.Compact(ids) {
		out = append(out, item{id})
	}
	ok(w, map[string]any{"data": out})
}

// --- models ---

type endpointInput struct {
	ProviderID    int64  `json:"provider_id"`
	UpstreamModel string `json:"upstream_model"`
	Priority      int    `json:"priority"`
	Enabled       *bool  `json:"enabled"`
}

type modelInput struct {
	Slug          *string          `json:"slug"`
	Name          *string          `json:"name"`
	Description   *string          `json:"description"`
	ContextLength *int             `json:"context_length"`
	InputPrice    *float64         `json:"input_price"`
	OutputPrice   *float64         `json:"output_price"`
	Enabled       *bool            `json:"enabled"`
	Endpoints     *[]endpointInput `json:"endpoints"`
}

func (in *modelInput) validate(creating bool) string {
	if in.Slug != nil {
		*in.Slug = strings.TrimSpace(*in.Slug)
		if *in.Slug == "" || strings.ContainsAny(*in.Slug, " \t\n") {
			return "slug must be non-empty and contain no spaces"
		}
	} else if creating {
		return "slug is required"
	}
	if (in.InputPrice != nil && *in.InputPrice < 0) || (in.OutputPrice != nil && *in.OutputPrice < 0) {
		return "prices must be ≥ 0"
	}
	if in.ContextLength != nil && *in.ContextLength < 0 {
		return "context_length must be ≥ 0"
	}
	if in.Endpoints != nil {
		for _, e := range *in.Endpoints {
			if e.ProviderID == 0 || strings.TrimSpace(e.UpstreamModel) == "" {
				return "each endpoint needs provider_id and upstream_model"
			}
		}
	}
	return ""
}

func (in *modelInput) endpoints() []store.Endpoint {
	if in.Endpoints == nil {
		return nil
	}
	out := []store.Endpoint{}
	for _, e := range *in.Endpoints {
		enabled := e.Enabled == nil || *e.Enabled
		out = append(out, store.Endpoint{ProviderID: e.ProviderID, UpstreamModel: strings.TrimSpace(e.UpstreamModel),
			Priority: e.Priority, Enabled: enabled})
	}
	return out
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	ms, err := s.store.ListModels(r.Context(), false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]modelJSON, 0, len(ms))
	for i := range ms {
		out = append(out, modelDTO(&ms[i]))
	}
	ok(w, map[string]any{"data": out})
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var in modelInput
	if !decode(w, r, &in) {
		return
	}
	if msg := in.validate(true); msg != "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}
	m := store.Model{Slug: *in.Slug, Enabled: true, Endpoints: in.endpoints()}
	if in.Name != nil {
		m.Name = *in.Name
	}
	if m.Name == "" {
		m.Name = m.Slug
	}
	if in.Description != nil {
		m.Description = *in.Description
	}
	if in.ContextLength != nil {
		m.ContextLength = *in.ContextLength
	}
	if in.InputPrice != nil {
		m.InputPrice = *in.InputPrice
	}
	if in.OutputPrice != nil {
		m.OutputPrice = *in.OutputPrice
	}
	if in.Enabled != nil {
		m.Enabled = *in.Enabled
	}
	created, err := s.store.CreateModel(r.Context(), m)
	if err != nil {
		s.modelFail(w, r, err)
		return
	}
	ok(w, map[string]any{"model": modelDTO(created)})
}

func (s *Server) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in modelInput
	if !decode(w, r, &in) {
		return
	}
	if msg := in.validate(false); msg != "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}
	upd := store.ModelUpdate{Slug: in.Slug, Name: in.Name, Description: in.Description, ContextLength: in.ContextLength,
		InputPrice: in.InputPrice, OutputPrice: in.OutputPrice, Enabled: in.Enabled}
	if in.Endpoints != nil {
		eps := in.endpoints()
		upd.Endpoints = &eps
	}
	m, err := s.store.UpdateModel(r.Context(), id, upd)
	if err != nil {
		s.modelFail(w, r, err)
		return
	}
	ok(w, map[string]any{"model": modelDTO(m)})
}

func (s *Server) modelFail(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(err.Error(), "FOREIGN KEY") {
		writeErr(w, http.StatusBadRequest, "invalid_request", "endpoint refers to an unknown provider")
		return
	}
	s.fail(w, r, err)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteModel(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"ok": true})
}

func (s *Server) handleImportModels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProviderID     int64    `json:"provider_id"`
		UpstreamModels []string `json:"upstream_models"`
		SlugPrefix     string   `json:"slug_prefix"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, err := s.store.GetProvider(r.Context(), in.ProviderID); err != nil {
		s.fail(w, r, err)
		return
	}
	created, skipped, err := s.store.ImportModels(r.Context(), in.ProviderID, in.SlugPrefix, in.UpstreamModels)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"created": created, "skipped": skipped})
}

// --- users ---

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, total, err := s.store.ListUsers(r.Context(), r.URL.Query().Get("q"), pageOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type row struct {
		userJSON
		TotalCost float64 `json:"total_cost"`
		Requests  int64   `json:"requests"`
	}
	out := make([]row, 0, len(users))
	for i := range users {
		out = append(out, row{userDTO(&users[i].User), store.ToUSD(users[i].TotalCost), users[i].Requests})
	}
	paged(w, out, total)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in struct {
		Name     *string `json:"name"`
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role != nil && *in.Role != store.RoleAdmin && *in.Role != store.RoleUser {
		writeErr(w, http.StatusBadRequest, "invalid_request", "role must be admin or user")
		return
	}
	if id == currentUser(r).ID && ((in.Role != nil && *in.Role != store.RoleAdmin) || (in.Disabled != nil && *in.Disabled)) {
		writeErr(w, http.StatusBadRequest, "invalid_request", "you cannot demote or disable yourself")
		return
	}
	u, err := s.store.UpdateUser(r.Context(), id, store.UserUpdate{Name: in.Name, Role: in.Role, Disabled: in.Disabled})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"user": userDTO(u)})
}

func (s *Server) handleAdjustCredits(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in struct {
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Amount == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "amount must not be zero")
		return
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		note = "Adjusted by " + currentUser(r).Email
	}
	u, err := s.store.AdjustBalance(r.Context(), id, store.FromUSD(in.Amount), note)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"user": userDTO(u)})
}

func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	logs, total, err := s.store.ListLogs(r.Context(), store.LogFilter{UserID: userID, Model: r.URL.Query().Get("model")}, pageOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]logJSON, 0, len(logs))
	for i := range logs {
		out = append(out, logDTO(&logs[i], true))
	}
	paged(w, out, total)
}

// --- redeem codes ---

func (s *Server) handleListCodes(w http.ResponseWriter, r *http.Request) {
	codes, total, err := s.store.ListRedeemCodes(r.Context(), pageOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type codeJSON struct {
		ID          int64      `json:"id"`
		Code        string     `json:"code"`
		Amount      float64    `json:"amount"`
		UsedByEmail *string    `json:"used_by_email"`
		UsedAt      *time.Time `json:"used_at"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	out := make([]codeJSON, 0, len(codes))
	for _, c := range codes {
		out = append(out, codeJSON{c.ID, c.Code, store.ToUSD(c.Amount), c.UsedByEmail, c.UsedAt, c.CreatedAt})
	}
	paged(w, out, total)
}

func newRedeemCode() string {
	h := strings.ToUpper(secure.RandomHex(8))
	return "PLEX-" + h[0:4] + "-" + h[4:8] + "-" + h[8:12] + "-" + h[12:16]
}

func (s *Server) handleCreateCodes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Amount float64 `json:"amount"`
		Count  int     `json:"count"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Amount <= 0 || in.Count < 1 || in.Count > 100 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "amount must be > 0 and count between 1 and 100")
		return
	}
	codes := make([]string, in.Count)
	for i := range codes {
		codes[i] = newRedeemCode()
	}
	if err := s.store.CreateRedeemCodes(r.Context(), codes, store.FromUSD(in.Amount)); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"codes": codes})
}

func (s *Server) handleDeleteCode(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteRedeemCode(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"ok": true})
}
