package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Provider key statuses. Failed statuses are set automatically by the
// gateway and keep the key out of rotation until an admin re-enables it.
const (
	KeyActive        = "active"
	KeyAuthFailed    = "auth_failed"
	KeyQuotaExceeded = "quota_exceeded"
)

// ProviderKey is one upstream API key of a provider. KeyEnc is encrypted
// with secure.Box.
type ProviderKey struct {
	ID          int64
	ProviderID  int64
	Name        string
	KeyEnc      string
	Enabled     bool
	Status      string
	LastError   string
	LastErrorAt *time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time
}

// Usable reports whether the key may be used (ignoring temporary cooldowns).
func (k *ProviderKey) Usable() bool { return k.Enabled && k.Status == KeyActive }

const providerKeyCols = `id, provider_id, name, key_enc, enabled, status, last_error, last_error_at, last_used_at, created_at`

func scanProviderKey(row interface{ Scan(...any) error }) (*ProviderKey, error) {
	var k ProviderKey
	var errAt, usedAt sql.NullInt64
	var created int64
	err := row.Scan(&k.ID, &k.ProviderID, &k.Name, &k.KeyEnc, &k.Enabled, &k.Status, &k.LastError, &errAt, &usedAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.LastErrorAt, k.LastUsedAt = nullTime(errAt), nullTime(usedAt)
	k.CreatedAt = unixTime(created)
	return &k, nil
}

// ListProviderKeys returns the keys of one provider, or of all providers
// when providerID is 0, oldest first.
func (s *Store) ListProviderKeys(ctx context.Context, providerID int64) ([]ProviderKey, error) {
	q, args := `SELECT `+providerKeyCols+` FROM provider_keys`, []any{}
	if providerID != 0 {
		q, args = q+` WHERE provider_id = ?`, append(args, providerID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderKey{}
	for rows.Next() {
		k, err := scanProviderKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (s *Store) GetProviderKey(ctx context.Context, providerID, id int64) (*ProviderKey, error) {
	return scanProviderKey(s.db.QueryRowContext(ctx, `SELECT `+providerKeyCols+` FROM provider_keys
		WHERE id = ? AND provider_id = ?`, id, providerID))
}

// AddProviderKeys stores encrypted keys for a provider and returns them.
func (s *Store) AddProviderKeys(ctx context.Context, providerID int64, names, encKeys []string) ([]ProviderKey, error) {
	var ids []int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		for i, enc := range encKeys {
			res, err := tx.ExecContext(ctx, `INSERT INTO provider_keys (provider_id, name, key_enc, created_at) VALUES (?, ?, ?, ?)`,
				providerID, names[i], enc, now())
			if err != nil {
				if strings.Contains(err.Error(), "FOREIGN KEY") {
					return ErrNotFound
				}
				return err
			}
			id, _ := res.LastInsertId()
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]ProviderKey, 0, len(ids))
	for _, id := range ids {
		k, err := s.GetProviderKey(ctx, providerID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, nil
}

type ProviderKeyUpdate struct {
	Name    *string
	Enabled *bool // enabling also clears a failed status
}

func (s *Store) UpdateProviderKey(ctx context.Context, providerID, id int64, upd ProviderKeyUpdate) (*ProviderKey, error) {
	sets, args := []string{}, []any{}
	if upd.Name != nil {
		sets, args = append(sets, "name = ?"), append(args, *upd.Name)
	}
	if upd.Enabled != nil {
		sets, args = append(sets, "enabled = ?"), append(args, boolInt(*upd.Enabled))
		if *upd.Enabled {
			sets, args = append(sets, "status = ?", "last_error = ''"), append(args, KeyActive)
		}
	}
	if len(sets) > 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE provider_keys SET `+strings.Join(sets, ", ")+` WHERE id = ? AND provider_id = ?`,
			append(args, id, providerID)...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	return s.GetProviderKey(ctx, providerID, id)
}

func (s *Store) DeleteProviderKey(ctx context.Context, providerID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM provider_keys WHERE id = ? AND provider_id = ?`, id, providerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FailProviderKey takes a key out of rotation with a failed status.
func (s *Store) FailProviderKey(ctx context.Context, id int64, status, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE provider_keys SET status = ?, last_error = ?, last_error_at = ? WHERE id = ?`,
		status, message, now(), id)
	return err
}

// NoteProviderKeyError records the latest error of a key that stays in use.
func (s *Store) NoteProviderKeyError(ctx context.Context, id int64, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE provider_keys SET last_error = ?, last_error_at = ? WHERE id = ?`, message, now(), id)
	return err
}
