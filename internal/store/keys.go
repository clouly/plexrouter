package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type APIKey struct {
	ID          int64
	UserID      int64
	Name        string
	Prefix      string
	CreditLimit *int64 // nano-USD; nil = unlimited
	Usage       int64  // nano-USD
	Disabled    bool
	CreatedAt   time.Time
	LastUsedAt  *time.Time
}

const keyCols = `id, user_id, name, prefix, credit_limit, usage, disabled, created_at, last_used_at`

func scanKey(row interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	var limit, lastUsed sql.NullInt64
	var created int64
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &limit, &k.Usage, &k.Disabled, &created, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if limit.Valid {
		k.CreditLimit = &limit.Int64
	}
	k.CreatedAt = unixTime(created)
	k.LastUsedAt = nullTime(lastUsed)
	return &k, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, name, keyHash, prefix string, limit *int64) (*APIKey, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, name, key_hash, prefix, credit_limit, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, keyHash, prefix, limit, now())
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
	Name        *string
	CreditLimit **int64 // set to a pointer-to-nil to clear the limit
	Disabled    *bool
}

func (s *Store) UpdateAPIKey(ctx context.Context, userID, id int64, upd APIKeyUpdate) (*APIKey, error) {
	sets, args := []string{}, []any{}
	if upd.Name != nil {
		sets, args = append(sets, "name = ?"), append(args, *upd.Name)
	}
	if upd.CreditLimit != nil {
		sets, args = append(sets, "credit_limit = ?"), append(args, *upd.CreditLimit)
	}
	if upd.Disabled != nil {
		sets, args = append(sets, "disabled = ?"), append(args, boolInt(*upd.Disabled))
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
	row := s.db.QueryRowContext(ctx, `
		SELECT k.id, k.user_id, k.name, k.prefix, k.credit_limit, k.usage, k.disabled, k.created_at, k.last_used_at,
		       u.id, u.email, u.name, u.password_hash, u.role, u.balance, u.disabled, u.created_at
		FROM api_keys k JOIN users u ON u.id = k.user_id WHERE k.key_hash = ?`, keyHash)
	var k APIKey
	var u User
	var limit, lastUsed sql.NullInt64
	var kCreated, uCreated int64
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &limit, &k.Usage, &k.Disabled, &kCreated, &lastUsed,
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Balance, &u.Disabled, &uCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if limit.Valid {
		k.CreditLimit = &limit.Int64
	}
	k.CreatedAt, u.CreatedAt = unixTime(kCreated), unixTime(uCreated)
	k.LastUsedAt = nullTime(lastUsed)
	return &k, &u, nil
}
