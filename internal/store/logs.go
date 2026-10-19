package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type RequestLog struct {
	ID               int64
	UserID           int64
	UserEmail        string // filled by admin listing only
	APIKeyID         int64
	APIKeyName       string
	Model            string
	ProviderID       int64
	ProviderName     string
	ProviderKeyID    int64
	ProviderKeyName  string
	PromptTokens     int
	CompletionTokens int
	Cost             int64  // nano-USD charged to the user
	UpstreamCost     *int64 // nano-USD paid upstream; nil = endpoint has no cost configured
	LatencyMS        int64
	Status           string // "success" | "error"
	Error            string
	Stream           bool
	CreatedAt        time.Time
}

// RecordRequest stores a request log and, in the same transaction, charges its
// cost to the user's balance and the key's usage.
func (s *Store) RecordRequest(ctx context.Context, l RequestLog) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		t := now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO request_logs
			(user_id, api_key_id, api_key_name, model, provider_id, provider_name, provider_key_id, provider_key_name,
			 prompt_tokens, completion_tokens, cost, upstream_cost, latency_ms, status, error, stream, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.UserID, l.APIKeyID, l.APIKeyName, l.Model, l.ProviderID, l.ProviderName, l.ProviderKeyID, l.ProviderKeyName,
			l.PromptTokens, l.CompletionTokens, l.Cost, l.UpstreamCost, l.LatencyMS, l.Status, l.Error, boolInt(l.Stream), t); err != nil {
			return err
		}
		if l.ProviderKeyID != 0 && l.Status == "success" {
			if _, err := tx.ExecContext(ctx, `UPDATE provider_keys SET last_used_at = ? WHERE id = ?`, t, l.ProviderKeyID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET usage = usage + ?, last_used_at = ? WHERE id = ?`,
			l.Cost, t, l.APIKeyID); err != nil {
			return err
		}
		if l.Cost != 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET balance = balance - ? WHERE id = ?`, l.Cost, l.UserID); err != nil {
				return err
			}
		}
		return nil
	})
}

type LogFilter struct {
	UserID int64 // 0 = all users
	Model  string
}

func (s *Store) ListLogs(ctx context.Context, f LogFilter, p Page) ([]RequestLog, int, error) {
	conds, args := []string{"1 = 1"}, []any{}
	if f.UserID != 0 {
		conds, args = append(conds, "l.user_id = ?"), append(args, f.UserID)
	}
	if f.Model != "" {
		conds, args = append(conds, "l.model = ?"), append(args, f.Model)
	}
	where := " WHERE " + strings.Join(conds, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs l`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.user_id, COALESCE(u.email, ''), l.api_key_id, l.api_key_name, l.model, l.provider_id, l.provider_name,
		       l.provider_key_id, l.provider_key_name, l.prompt_tokens, l.completion_tokens, l.cost, l.upstream_cost,
		       l.latency_ms, l.status, l.error, l.stream, l.created_at
		FROM request_logs l LEFT JOIN users u ON u.id = l.user_id`+where+`
		ORDER BY l.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []RequestLog{}
	for rows.Next() {
		var l RequestLog
		var created int64
		var upstream sql.NullInt64
		if err := rows.Scan(&l.ID, &l.UserID, &l.UserEmail, &l.APIKeyID, &l.APIKeyName, &l.Model, &l.ProviderID, &l.ProviderName,
			&l.ProviderKeyID, &l.ProviderKeyName, &l.PromptTokens, &l.CompletionTokens, &l.Cost, &upstream,
			&l.LatencyMS, &l.Status, &l.Error, &l.Stream, &created); err != nil {
			return nil, 0, err
		}
		if upstream.Valid {
			l.UpstreamCost = &upstream.Int64
		}
		l.CreatedAt = unixTime(created)
		out = append(out, l)
	}
	return out, total, rows.Err()
}

type DailyStat struct {
	Date     string // YYYY-MM-DD (UTC)
	Requests int64
	Tokens   int64
	Cost     int64
}

type ModelStat struct {
	Model    string
	Requests int64
	Tokens   int64
	Cost     int64
}

type UsageStats struct {
	Requests, Tokens, Cost int64
	Daily                  []DailyStat // one per day, oldest first, zero-filled
	ByModel                []ModelStat // most expensive first
}

// Stats aggregates the last `days` days (including today, UTC). userID 0 = all.
func (s *Store) Stats(ctx context.Context, userID int64, days int) (*UsageStats, error) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	cond, args := "created_at >= ?", []any{start.Unix()}
	if userID != 0 {
		cond, args = cond+" AND user_id = ?", append(args, userID)
	}

	st := &UsageStats{ByModel: []ModelStat{}}
	byDate := map[string]DailyStat{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(created_at, 'unixepoch'), COUNT(*), COALESCE(SUM(prompt_tokens + completion_tokens), 0), COALESCE(SUM(cost), 0)
		FROM request_logs WHERE `+cond+` GROUP BY 1`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DailyStat
		if err := rows.Scan(&d.Date, &d.Requests, &d.Tokens, &d.Cost); err != nil {
			rows.Close()
			return nil, err
		}
		byDate[d.Date] = d
		st.Requests += d.Requests
		st.Tokens += d.Tokens
		st.Cost += d.Cost
	}
	rows.Close()
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		d, ok := byDate[date]
		if !ok {
			d = DailyStat{Date: date}
		}
		st.Daily = append(st.Daily, d)
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT model, COUNT(*), COALESCE(SUM(prompt_tokens + completion_tokens), 0), COALESCE(SUM(cost), 0)
		FROM request_logs WHERE `+cond+` GROUP BY model ORDER BY 4 DESC, 2 DESC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m ModelStat
		if err := rows.Scan(&m.Model, &m.Requests, &m.Tokens, &m.Cost); err != nil {
			return nil, err
		}
		st.ByModel = append(st.ByModel, m)
	}
	return st, rows.Err()
}
