package store

import (
	"context"
	"database/sql"
	"time"
)

// ReportRow aggregates requests. Revenue is what users were charged;
// UpstreamCost is what the operator paid providers, known only for endpoints
// with costs configured; Profit counts only requests with a known cost.
type ReportRow struct {
	Requests            int64
	Tokens              int64
	Revenue             int64 // nano-USD
	UpstreamCost        int64
	Profit              int64
	UnknownCostRequests int64 // successful requests without a configured cost
}

const reportCols = `COUNT(*),
	COALESCE(SUM(prompt_tokens + completion_tokens), 0),
	COALESCE(SUM(cost), 0),
	COALESCE(SUM(upstream_cost), 0),
	COALESCE(SUM(CASE WHEN upstream_cost IS NOT NULL THEN cost - upstream_cost ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = 'success' AND upstream_cost IS NULL THEN 1 ELSE 0 END), 0)`

func (r *ReportRow) dest() []any {
	return []any{&r.Requests, &r.Tokens, &r.Revenue, &r.UpstreamCost, &r.Profit, &r.UnknownCostRequests}
}

type DailyReport struct {
	Date string
	ReportRow
}

type ModelReport struct {
	Model string
	ReportRow
}

type ProviderReport struct {
	ProviderID  int64
	Provider    string
	SuccessRate *float64
	ReportRow
}

type Report struct {
	Totals     ReportRow
	Daily      []DailyReport // one per day, oldest first, zero-filled
	ByModel    []ModelReport
	ByProvider []ProviderReport
}

// Report aggregates the last `days` days (including today, UTC) for admins.
func (s *Store) Report(ctx context.Context, days int) (*Report, error) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1)).Unix()
	rep := &Report{ByModel: []ModelReport{}, ByProvider: []ProviderReport{}}

	if err := s.db.QueryRowContext(ctx, `SELECT `+reportCols+` FROM request_logs WHERE created_at >= ?`, start).
		Scan(rep.Totals.dest()...); err != nil {
		return nil, err
	}

	byDate := map[string]ReportRow{}
	rows, err := s.db.QueryContext(ctx, `SELECT date(created_at, 'unixepoch'), `+reportCols+`
		FROM request_logs WHERE created_at >= ? GROUP BY 1`, start)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DailyReport
		if err := rows.Scan(append([]any{&d.Date}, d.dest()...)...); err != nil {
			rows.Close()
			return nil, err
		}
		byDate[d.Date] = d.ReportRow
	}
	rows.Close()
	for i := 0; i < days; i++ {
		date := time.Unix(start, 0).UTC().AddDate(0, 0, i).Format("2006-01-02")
		rep.Daily = append(rep.Daily, DailyReport{Date: date, ReportRow: byDate[date]})
	}

	rows, err = s.db.QueryContext(ctx, `SELECT model, `+reportCols+` FROM request_logs
		WHERE created_at >= ? GROUP BY model ORDER BY 4 DESC, 2 DESC LIMIT 100`, start)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m ModelReport
		if err := rows.Scan(append([]any{&m.Model}, m.dest()...)...); err != nil {
			rows.Close()
			return nil, err
		}
		rep.ByModel = append(rep.ByModel, m)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT provider_id, MAX(provider_name),
		SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), `+reportCols+`
		FROM request_logs WHERE created_at >= ? AND provider_id != 0 GROUP BY provider_id ORDER BY 6 DESC`, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ProviderReport
		var ok int64
		if err := rows.Scan(append([]any{&p.ProviderID, &p.Provider, &ok}, p.dest()...)...); err != nil {
			return nil, err
		}
		if p.Requests > 0 {
			rate := float64(ok) / float64(p.Requests)
			p.SuccessRate = &rate
		}
		rep.ByProvider = append(rep.ByProvider, p)
	}
	return rep, rows.Err()
}

// Health is request success over a recent window.
type Health struct {
	Requests     int64
	Successes    int64
	AvgLatencyMS *float64 // successful requests only
}

func (h Health) SuccessRate() *float64 {
	if h.Requests == 0 {
		return nil
	}
	r := float64(h.Successes) / float64(h.Requests)
	return &r
}

// ProviderHealth returns per-provider health since `since`.
func (s *Store) ProviderHealth(ctx context.Context, since time.Time) (map[int64]Health, error) {
	return s.health(ctx, "provider_id", since)
}

// ProviderKeyHealth returns per-provider-key health since `since`.
func (s *Store) ProviderKeyHealth(ctx context.Context, since time.Time) (map[int64]Health, error) {
	return s.health(ctx, "provider_key_id", since)
}

func (s *Store) health(ctx context.Context, col string, since time.Time) (map[int64]Health, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+col+`, COUNT(*),
		SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END),
		AVG(CASE WHEN status = 'success' THEN latency_ms END)
		FROM request_logs WHERE created_at >= ? AND `+col+` != 0 GROUP BY `+col, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Health{}
	for rows.Next() {
		var id int64
		var h Health
		var avg sql.NullFloat64
		if err := rows.Scan(&id, &h.Requests, &h.Successes, &avg); err != nil {
			return nil, err
		}
		if avg.Valid {
			h.AvgLatencyMS = &avg.Float64
		}
		out[id] = h
	}
	return out, rows.Err()
}

// PurgeLogs deletes request logs older than `before`.
func (s *Store) PurgeLogs(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM request_logs WHERE created_at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
