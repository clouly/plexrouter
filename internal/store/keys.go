package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type APIKey struct {
	ID            int64
	UserID        int64
	Name          string
	Prefix        string
	CreditLimit   *int64 // nano-USD; nil = unlimited
	Usage         int64  // nano-USD
	RateLimitRPM  *int   // requests/minute; nil = no key-level limit
	AllowedModels []string
	Disabled      bool
	CreatedAt     time.Time
	LastUsedAt    *time.Time
}

// AllowsModel reports whether the key may call a model slug.
func (k *APIKey) AllowsModel(slug string) bool {
	if len(k.AllowedModels) == 0 {
		return true
	}
	for _, m := range k.AllowedModels {
		if m == slug {
			return true
		}
	}
	return false
}

const (
	keyCols  = `id, user_id, name, prefix, credit_limit, usage, rate_limit_rpm, allowed_models, disabled, created_at, last_used_at`
	keyColsK = `k.id, k.user_id, k.name, k.prefix, k.credit_limit, k.usage, k.rate_limit_rpm, k.allowed_models, k.disabled, k.created_at, k.last_used_at`
)

// scanKey reads keyCols, followed by any extra columns into extra.
func scanKey(row interface{ Scan(...any) error }, extra ...any) (*APIKey, error) {
	var k APIKey
	var limit, rpm, lastUsed sql.NullInt64
	var allowed string
	var created int64
	dest := append([]any{&k.ID, &k.UserID, &k.Name, &k.Prefix, &limit, &k.Usage, &rpm, &allowed, &k.Disabled, &created, &lastUsed}, extra...)
	err := row.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if limit.Valid {
		k.CreditLimit = &limit.Int64
	}
	if rpm.Valid {
		n := int(rpm.Int64)
		k.RateLimitRPM = &n
	}
	k.AllowedModels = decodeList(allowed)
	k.CreatedAt = unixTime(created)
	k.LastUsedAt = nullTime(lastUsed)
	return &k, nil
}

// Lists of short strings are stored as a JSON array ("" = empty).
func encodeList(l []string) string {
	if len(l) == 0 {
		return ""
	}
	b, _ := json.Marshal(l)
	return string(b)
}

func decodeList(s string) []string {
	out := []string{}
	if s != "" {
		json.Unmarshal([]byte(s), &out)
	}
	return out
}

type NewAPIKey struct {
	Name          string
	KeyHash       string
	Prefix        string
	CreditLimit   *int64
	RateLimitRPM  *int
	AllowedModels []string
}

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, nk NewAPIKey) (*APIKey, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, name, key_hash, prefix, credit_limit, rate_limit_rpm, allowed_models, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, nk.Name, nk.KeyHash, nk.Prefix, nk.CreditLimit, nk.RateLimitRPM, encodeList(nk.AllowedModels), now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetAPIKey(ctx, userID, id)
}

func (s *Store) GetAPIKey(ctx context.Context, userID, id int64) (*APIKey, error) {
	return scanKey(s.db.QueryRowContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

type APIKeyUpdate struct {
	Name          *string
	CreditLimit   **int64 // set to a pointer-to-nil to clear the limit
	RateLimitRPM  **int   // likewise
	AllowedModels *[]string
	Disabled      *bool
}

func (s *Store) UpdateAPIKey(ctx context.Context, userID, id int64, upd APIKeyUpdate) (*APIKey, error) {
	sets, args := []string{}, []any{}
	add := func(col string, v any) { sets, args = append(sets, col+" = ?"), append(args, v) }
	if upd.Name != nil {
		add("name", *upd.Name)
	}
	if upd.CreditLimit != nil {
		add("credit_limit", *upd.CreditLimit)
	}
	if upd.RateLimitRPM != nil {
		add("rate_limit_rpm", *upd.RateLimitRPM)
	}
	if upd.AllowedModels != nil {
		add("allowed_models", encodeList(*upd.AllowedModels))
	}
	if upd.Disabled != nil {
		add("disabled", boolInt(*upd.Disabled))
	}
	if len(sets) > 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET `+strings.Join(sets, ", ")+` WHERE id = ? AND user_id = ?`,
			append(args, id, userID)...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	return s.GetAPIKey(ctx, userID, id)
}

func (s *Store) DeleteAPIKey(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// KeyAuth resolves an API key hash to the key and its owner.
func (s *Store) KeyAuth(ctx context.Context, keyHash string) (*APIKey, *User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+keyColsK+`, `+userColsU+`
		FROM api_keys k JOIN users u ON u.id = k.user_id WHERE k.key_hash = ?`, keyHash)
	var u User
	var uCreated int64
	var uRPM sql.NullInt64
	k, err := scanKey(row, &u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Balance, &u.Disabled, &uCreated, &uRPM)
	if err != nil {
		return nil, nil, err
	}
	u.CreatedAt = unixTime(uCreated)
	if uRPM.Valid {
		n := int(uRPM.Int64)
		u.RateLimitRPM = &n
	}
	return k, &u, nil
}
