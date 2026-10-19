package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local4a/plexrouter/internal/gateway"
	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/store"
)

func (s *Server) handlePublicModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListModels(r.Context(), true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []publicModelJSON{}
	for _, m := range models {
		if len(m.Endpoints) == 0 {
			continue
		}
		out = append(out, publicModelJSON{m.Slug, m.Name, m.Description, m.ContextLength, m.InputPrice, m.OutputPrice, len(m.Endpoints)})
	}
	ok(w, map[string]any{"data": out})
}

// --- API keys ---

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListAPIKeys(r.Context(), currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]keyJSON, 0, len(keys))
	for i := range keys {
		out = append(out, keyDTO(&keys[i]))
	}
	ok(w, map[string]any{"data": out})
}

func usdLimit(v *float64) (*int64, bool) {
	if v == nil {
		return nil, true
	}
	if *v < 0 {
		return nil, false
	}
	n := store.FromUSD(*v)
	return &n, true
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string   `json:"name"`
		CreditLimit *float64 `json:"credit_limit"`
	}
	if !decode(w, r, &in) {
		return
	}
	limit, valid := usdLimit(in.CreditLimit)
	if !valid {
		writeErr(w, http.StatusBadRequest, "invalid_request", "credit_limit must be ≥ 0")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Key " + time.Now().UTC().Format("2006-01-02")
	}
	secret := gateway.KeyPrefix + secure.RandomHex(24)
	prefix := secret[:len(gateway.KeyPrefix)+6] + "…"
	k, err := s.store.CreateAPIKey(r.Context(), currentUser(r).ID, name, secure.HashToken(secret), prefix, limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"key": keyDTO(k), "secret": secret})
}

func (s *Server) handleUpdateKey(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	var in struct {
		Name        *string            `json:"name"`
		CreditLimit Optional[*float64] `json:"credit_limit"`
		Disabled    *bool              `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	upd := store.APIKeyUpdate{Name: in.Name, Disabled: in.Disabled}
	if in.CreditLimit.Set {
		limit, valid := usdLimit(in.CreditLimit.Value)
		if !valid {
			writeErr(w, http.StatusBadRequest, "invalid_request", "credit_limit must be ≥ 0")
			return
		}
		upd.CreditLimit = &limit
	}
	k, err := s.store.UpdateAPIKey(r.Context(), currentUser(r).ID, id, upd)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"key": keyDTO(k)})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(w, r)
	if !valid {
		return
	}
	if err := s.store.DeleteAPIKey(r.Context(), currentUser(r).ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"ok": true})
}

// --- usage ---

func (s *Server) handleUsageLogs(w http.ResponseWriter, r *http.Request) {
	logs, total, err := s.store.ListLogs(r.Context(),
		store.LogFilter{UserID: currentUser(r).ID, Model: r.URL.Query().Get("model")}, pageOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]logJSON, 0, len(logs))
	for i := range logs {
		out = append(out, logDTO(&logs[i], false))
	}
	paged(w, out, total)
}

func (s *Server) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 7
	}
	days = min(days, 90)
	st, err := s.store.Stats(r.Context(), currentUser(r).ID, days)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type modelStat struct {
		Model    string  `json:"model"`
		Requests int64   `json:"requests"`
		Tokens   int64   `json:"tokens"`
		Cost     float64 `json:"cost"`
	}
	byModel := make([]modelStat, 0, len(st.ByModel))
	for _, m := range st.ByModel {
		byModel = append(byModel, modelStat{m.Model, m.Requests, m.Tokens, store.ToUSD(m.Cost)})
	}
	ok(w, map[string]any{
		"total_cost": store.ToUSD(st.Cost), "total_requests": st.Requests, "total_tokens": st.Tokens,
		"daily": dailyDTO(st.Daily), "by_model": byModel,
	})
}

// --- credits ---

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	txs, total, err := s.store.ListTransactions(r.Context(), currentUser(r).ID, pageOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type txJSON struct {
		ID        int64     `json:"id"`
		Amount    float64   `json:"amount"`
		Type      string    `json:"type"`
		Note      string    `json:"note"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := make([]txJSON, 0, len(txs))
	for _, t := range txs {
		out = append(out, txJSON{t.ID, store.ToUSD(t.Amount), t.Type, t.Note, t.CreatedAt})
	}
	paged(w, out, total)
}

func (s *Server) handleRedeem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.limiter.allow("redeem:" + strconv.FormatInt(currentUser(r).ID, 10)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again in a minute")
		return
	}
	amount, balance, err := s.store.Redeem(r.Context(), currentUser(r).ID, strings.ToUpper(strings.TrimSpace(in.Code)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"amount": store.ToUSD(amount), "balance": store.ToUSD(balance)})
}
