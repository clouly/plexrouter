package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Identity links a user to an external sign-in provider (Google, GitHub).
type Identity struct {
	UserID    int64
	Provider  string
	Subject   string // stable account ID at the provider
	Email     string
	CreatedAt time.Time
}

// IdentityUser returns the user linked to a provider account.
func (s *Store) IdentityUser(ctx context.Context, provider, subject string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.name, u.password_hash, u.role, u.balance, u.disabled, u.created_at
		FROM user_identities i JOIN users u ON u.id = i.user_id
		WHERE i.provider = ? AND i.subject = ?`, provider, subject))
}

// LinkIdentity attaches a provider account to a user, replacing any earlier
// account of the same provider on that user.
func (s *Store) LinkIdentity(ctx context.Context, id Identity) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_identities WHERE user_id = ? AND provider = ?`, id.UserID, id.Provider); err != nil {
			return err
		}
		return insertIdentity(ctx, tx, id)
	})
}

func insertIdentity(ctx context.Context, tx *sql.Tx, id Identity) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_identities (user_id, provider, subject, email, created_at) VALUES (?, ?, ?, ?, ?)`,
		id.UserID, id.Provider, id.Subject, id.Email, now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrIdentityTaken
	}
	return err
}

var ErrIdentityTaken = errors.New("this account is already linked to another user")

// CreateOAuthUser creates a password-less user together with its identity.
// Like CreateUser, the very first user becomes an admin.
func (s *Store) CreateOAuthUser(ctx context.Context, email, name string, signupCredit int64, id Identity) (*User, error) {
	var userID int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		userID, err = insertUser(ctx, tx, email, name, "", signupCredit)
		if err != nil {
			return err
		}
		id.UserID = userID
		return insertIdentity(ctx, tx, id)
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, userID)
}

func (s *Store) ListIdentities(ctx context.Context, userID int64) ([]Identity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, provider, subject, email, created_at FROM user_identities
		WHERE user_id = ? ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var i Identity
		var created int64
		if err := rows.Scan(&i.UserID, &i.Provider, &i.Subject, &i.Email, &created); err != nil {
			return nil, err
		}
		i.CreatedAt = unixTime(created)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) UnlinkIdentity(ctx context.Context, userID int64, provider string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_identities WHERE user_id = ? AND provider = ?`, userID, provider)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
