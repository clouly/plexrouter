package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"
)

// Model is what clients ask for ("openai/gpt-4o"). Prices are USD per 1M tokens.
type Model struct {
	ID              int64
	Slug            string
	Name            string
	Description     string
	ContextLength   int
	InputPrice      float64
	OutputPrice     float64
	CacheReadPrice  *float64 // nil = charged as input
	CacheWritePrice *float64
	Enabled         bool
	CreatedAt       time.Time
	Endpoints       []Endpoint
}

// Endpoint maps a model onto one provider. Lower priority is tried first.
type Endpoint struct {
	ID             int64
	ModelID        int64
	ProviderID     int64
	ProviderName   string
	UpstreamModel  string
	Priority       int
	Enabled        bool
	InputCost      *float64 // USD per 1M tokens paid upstream; nil = unknown
	OutputCost     *float64
	CacheReadCost  *float64 // nil = same as InputCost
	CacheWriteCost *float64
}

// TokenUsage is what one request consumed. Prompt includes the cached
// tokens (CacheRead) and tokens written to the cache (CacheWrite).
type TokenUsage struct {
	Prompt, Completion, CacheRead, CacheWrite int
}

// priceTokens prices usage in nano-USD; prices are USD per 1M tokens (so one
// token costs price*1000 nano-USD). Cache prices default to the input price.
func priceTokens(u TokenUsage, in, out float64, cacheRead, cacheWrite *float64) int64 {
	readP, writeP := in, in
	if cacheRead != nil {
		readP = *cacheRead
	}
	if cacheWrite != nil {
		writeP = *cacheWrite
	}
	uncached := max(u.Prompt-u.CacheRead-u.CacheWrite, 0)
	return int64(math.Round((float64(uncached)*in + float64(u.CacheRead)*readP + float64(u.CacheWrite)*writeP +
		float64(u.Completion)*out) * 1000))
}

// Charge is what a user pays for usage of this model, in nano-USD.
func (m *Model) Charge(u TokenUsage) int64 {
	return priceTokens(u, m.InputPrice, m.OutputPrice, m.CacheReadPrice, m.CacheWritePrice)
}

// UpstreamCost is what the operator pays for a request through this
// endpoint, in nano-USD, or nil if the endpoint has no cost configured.
func (e *Endpoint) UpstreamCost(u TokenUsage) *int64 {
	if e.InputCost == nil && e.OutputCost == nil {
		return nil
	}
	var in, out float64
	if e.InputCost != nil {
		in = *e.InputCost
	}
	if e.OutputCost != nil {
		out = *e.OutputCost
	}
	n := priceTokens(u, in, out, e.CacheReadCost, e.CacheWriteCost)
	return &n
}

var ErrSlugTaken = errors.New("model slug already exists")

const modelCols = `id, slug, name, description, context_length, input_price, output_price, cache_read_price, cache_write_price, enabled, created_at`

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func scanModel(row interface{ Scan(...any) error }) (*Model, error) {
	var m Model
	var created int64
	var readP, writeP sql.NullFloat64
	err := row.Scan(&m.ID, &m.Slug, &m.Name, &m.Description, &m.ContextLength, &m.InputPrice, &m.OutputPrice, &readP, &writeP, &m.Enabled, &created)
	m.CacheReadPrice, m.CacheWritePrice = nullFloat(readP), nullFloat(writeP)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = unixTime(created)
	m.Endpoints = []Endpoint{}
	return &m, nil
}

// ListModels returns models with their endpoints. With enabledOnly, disabled
// models and endpoints (or endpoints of disabled providers) are left out.
func (s *Store) ListModels(ctx context.Context, enabledOnly bool) ([]Model, error) {
	q := `SELECT ` + modelCols + ` FROM models`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	out := []Model{}
	index := map[int64]int{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[m.ID] = len(out)
		out = append(out, *m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	eps, err := s.listEndpoints(ctx, 0, enabledOnly)
	if err != nil {
		return nil, err
	}
	for _, e := range eps {
		if i, ok := index[e.ModelID]; ok {
			out[i].Endpoints = append(out[i].Endpoints, e)
		}
	}
	return out, nil
}

func (s *Store) listEndpoints(ctx context.Context, modelID int64, enabledOnly bool) ([]Endpoint, error) {
	q := `SELECT e.id, e.model_id, e.provider_id, p.name, e.upstream_model, e.priority, e.enabled, e.input_cost, e.output_cost,
		e.cache_read_cost, e.cache_write_cost
		FROM model_endpoints e JOIN providers p ON p.id = e.provider_id WHERE 1 = 1`
	var args []any
	if modelID != 0 {
		q += ` AND e.model_id = ?`
		args = append(args, modelID)
	}
	if enabledOnly {
		q += ` AND e.enabled = 1 AND p.enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY e.priority, e.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		var inCost, outCost, readCost, writeCost sql.NullFloat64
		if err := rows.Scan(&e.ID, &e.ModelID, &e.ProviderID, &e.ProviderName, &e.UpstreamModel, &e.Priority, &e.Enabled,
			&inCost, &outCost, &readCost, &writeCost); err != nil {
			return nil, err
		}
		e.InputCost, e.OutputCost = nullFloat(inCost), nullFloat(outCost)
		e.CacheReadCost, e.CacheWriteCost = nullFloat(readCost), nullFloat(writeCost)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetModel(ctx context.Context, id int64) (*Model, error) {
	m, err := scanModel(s.db.QueryRowContext(ctx, `SELECT `+modelCols+` FROM models WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	eps, err := s.listEndpoints(ctx, m.ID, false)
	if err != nil {
		return nil, err
	}
	m.Endpoints = append(m.Endpoints, eps...)
	return m, nil
}

// RouteModel returns an enabled model by slug with only its usable endpoints,
// in the order they should be tried.
func (s *Store) RouteModel(ctx context.Context, slug string) (*Model, error) {
	m, err := scanModel(s.db.QueryRowContext(ctx, `SELECT `+modelCols+` FROM models WHERE slug = ? AND enabled = 1`, slug))
	if err != nil {
		return nil, err
	}
	eps, err := s.listEndpoints(ctx, m.ID, true)
	if err != nil {
		return nil, err
	}
	m.Endpoints = append(m.Endpoints, eps...)
	return m, nil
}

func (s *Store) CreateModel(ctx context.Context, m Model) (*Model, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO models (slug, name, description, context_length, input_price, output_price,
			cache_read_price, cache_write_price, enabled, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Slug, m.Name, m.Description, m.ContextLength, m.InputPrice, m.OutputPrice,
			m.CacheReadPrice, m.CacheWritePrice, boolInt(m.Enabled), now())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrSlugTaken
			}
			return err
		}
		id, _ = res.LastInsertId()
		return insertEndpoints(ctx, tx, id, m.Endpoints)
	})
	if err != nil {
		return nil, err
	}
	return s.GetModel(ctx, id)
}

type ModelUpdate struct {
	Slug, Name, Description *string
	ContextLength           *int
	InputPrice, OutputPrice *float64
	CacheReadPrice          **float64 // pointer-to-nil clears (charged as input)
	CacheWritePrice         **float64
	Enabled                 *bool
	Endpoints               *[]Endpoint // replaces all endpoints when set
}

func (s *Store) UpdateModel(ctx context.Context, id int64, upd ModelUpdate) (*Model, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		sets, args := []string{}, []any{}
		add := func(col string, v any) { sets, args = append(sets, col+" = ?"), append(args, v) }
		if upd.Slug != nil {
			add("slug", *upd.Slug)
		}
		if upd.Name != nil {
			add("name", *upd.Name)
		}
		if upd.Description != nil {
			add("description", *upd.Description)
		}
		if upd.ContextLength != nil {
			add("context_length", *upd.ContextLength)
		}
		if upd.InputPrice != nil {
			add("input_price", *upd.InputPrice)
		}
		if upd.CacheReadPrice != nil {
			add("cache_read_price", *upd.CacheReadPrice)
		}
		if upd.CacheWritePrice != nil {
			add("cache_write_price", *upd.CacheWritePrice)
		}
		if upd.OutputPrice != nil {
			add("output_price", *upd.OutputPrice)
		}
		if upd.Enabled != nil {
			add("enabled", boolInt(*upd.Enabled))
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM models WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		if len(sets) > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE models SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, id)...); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					return ErrSlugTaken
				}
				return err
			}
		}
		if upd.Endpoints != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM model_endpoints WHERE model_id = ?`, id); err != nil {
				return err
			}
			return insertEndpoints(ctx, tx, id, *upd.Endpoints)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetModel(ctx, id)
}

func insertEndpoints(ctx context.Context, tx *sql.Tx, modelID int64, eps []Endpoint) error {
	for _, e := range eps {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO model_endpoints (model_id, provider_id, upstream_model, priority, enabled,
			  input_cost, output_cost, cache_read_cost, cache_write_cost)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			modelID, e.ProviderID, e.UpstreamModel, e.Priority, boolInt(e.Enabled),
			e.InputCost, e.OutputCost, e.CacheReadCost, e.CacheWriteCost); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteModel(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM models WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ImportModels creates a disabled, zero-priced model "prefix/upstream" for each
// upstream model of a provider. If the slug exists, the provider is added as a
// new endpoint unless it already serves that model.
func (s *Store) ImportModels(ctx context.Context, providerID int64, prefix string, upstream []string) (created, skipped int, err error) {
	prefix = strings.Trim(prefix, "/ ")
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		for _, up := range upstream {
			up = strings.TrimSpace(up)
			if up == "" {
				continue
			}
			slug := up
			if prefix != "" {
				slug = prefix + "/" + up
			}
			var modelID int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM models WHERE slug = ?`, slug).Scan(&modelID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				res, err := tx.ExecContext(ctx, `INSERT INTO models (slug, name, enabled, created_at) VALUES (?, ?, 0, ?)`, slug, up, now())
				if err != nil {
					return err
				}
				modelID, _ = res.LastInsertId()
				created++
			case err != nil:
				return err
			default:
				var n int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_endpoints WHERE model_id = ? AND provider_id = ?`,
					modelID, providerID).Scan(&n); err != nil {
					return err
				}
				if n > 0 {
					skipped++
					continue
				}
			}
			if err := insertEndpoints(ctx, tx, modelID, []Endpoint{{ProviderID: providerID, UpstreamModel: up, Enabled: true}}); err != nil {
				return err
			}
		}
		return nil
	})
	return created, skipped, err
}

func (s *Store) GetModelBySlug(ctx context.Context, slug string) (*Model, error) {
	m, err := scanModel(s.db.QueryRowContext(ctx, `SELECT `+modelCols+` FROM models WHERE slug = ?`, slug))
	if err != nil {
		return nil, err
	}
	return s.GetModel(ctx, m.ID)
}

// AddEndpoint attaches one more provider endpoint to a model.
func (s *Store) AddEndpoint(ctx context.Context, modelID int64, e Endpoint) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return insertEndpoints(ctx, tx, modelID, []Endpoint{e}) })
}

func (s *Store) CountModels(ctx context.Context) (models, providers int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM models WHERE enabled = 1), (SELECT COUNT(*) FROM providers WHERE enabled = 1)`).
		Scan(&models, &providers)
	return
}
