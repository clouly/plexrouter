package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Provider is an upstream account (OpenAI, Anthropic, a self-hosted vLLM…).
// APIKeyEnc is encrypted with secure.Box; callers decrypt it.
type Provider struct {
	ID        int64
	Name      string
	Type      string
	BaseURL   string
	APIKeyEnc string
	Enabled   bool
	CreatedAt time.Time
}

const providerCols = `id, name, type, base_url, api_key_enc, enabled, created_at`

func scanProvider(row interface{ Scan(...any) error }) (*Provider, error) {
	var p Provider
	var created int64
	err := row.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.APIKeyEnc, &p.Enabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt = unixTime(created)
	return &p, nil
}

func (s *Store) CreateProvider(ctx context.Context, p Provider) (*Provider, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO providers (name, type, base_url, api_key_enc, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.Type, p.BaseURL, p.APIKeyEnc, boolInt(p.Enabled), now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetProvider(ctx, id)
}

func (s *Store) GetProvider(ctx context.Context, id int64) (*Provider, error) {
	return scanProvider(s.db.QueryRowContext(ctx, `SELECT `+providerCols+` FROM providers WHERE id = ?`, id))
}

func (s *Store) ListProviders(ctx context.Context) ([]Provider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+providerCols+` FROM providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Provider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

type ProviderUpdate struct {
	Name, Type, BaseURL, APIKeyEnc *string
	Enabled                        *bool
}

func (s *Store) UpdateProvider(ctx context.Context, id int64, upd ProviderUpdate) (*Provider, error) {
	sets, args := []string{}, []any{}
	add := func(col string, v any) { sets, args = append(sets, col+" = ?"), append(args, v) }
	if upd.Name != nil {
		add("name", *upd.Name)
	}
	if upd.Type != nil {
		add("type", *upd.Type)
	}
	if upd.BaseURL != nil {
		add("base_url", *upd.BaseURL)
	}
	if upd.APIKeyEnc != nil {
		add("api_key_enc", *upd.APIKeyEnc)
	}
	if upd.Enabled != nil {
		add("enabled", boolInt(*upd.Enabled))
	}
	if len(sets) > 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE providers SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, id)...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	return s.GetProvider(ctx, id)
}

func (s *Store) DeleteProvider(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
