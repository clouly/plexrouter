package store

import "context"

// System settings changed in the console are stored as key/value strings.
// Keys that are absent fall back to environment variables and defaults.

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SaveSettings writes several settings at once; a nil value deletes the key
// (back to its environment/default value).
func (s *Store) SaveSettings(ctx context.Context, values map[string]*string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range values {
		if v == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, k)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, k, *v, now())
		}
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
