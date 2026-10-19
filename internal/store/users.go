package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type User struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
	Role         string
	Balance      int64 // nano-USD
	Disabled     bool
	CreatedAt    time.Time
}

func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

var ErrEmailTaken = errors.New("email already registered")

const userCols = `id, email, name, password_hash, role, balance, disabled, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Balance, &u.Disabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = unixTime(created)
	return &u, nil
}

// CreateUser inserts a user. The very first user becomes an admin. A positive
// signupCredit is granted and recorded as a transaction.
func (s *Store) CreateUser(ctx context.Context, email, name, passwordHash string, signupCredit int64) (*User, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}
		role := RoleUser
		if count == 0 {
			role = RoleAdmin
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO users (email, name, password_hash, role, balance, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			strings.TrimSpace(email), name, passwordHash, role, max(signupCredit, 0), now())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrEmailTaken
			}
			return err
		}
		id, _ = res.LastInsertId()
		if signupCredit > 0 {
			return insertTransaction(ctx, tx, id, signupCredit, TxSignup, "Signup bonus")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, strings.TrimSpace(email)))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

type UserWithStats struct {
	User
	TotalCost int64
	Requests  int64
}

func (s *Store) ListUsers(ctx context.Context, q string, p Page) ([]UserWithStats, int, error) {
	where, args := "", []any{}
	if q = strings.TrimSpace(q); q != "" {
		where = ` WHERE u.email LIKE ? OR u.name LIKE ?`
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.name, u.password_hash, u.role, u.balance, u.disabled, u.created_at,
		       COALESCE(SUM(l.cost), 0), COUNT(l.id)
		FROM users u LEFT JOIN request_logs l ON l.user_id = u.id`+where+`
		GROUP BY u.id ORDER BY u.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []UserWithStats
	for rows.Next() {
		var u UserWithStats
		var created int64
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Balance, &u.Disabled, &created,
			&u.TotalCost, &u.Requests); err != nil {
			return nil, 0, err
		}
		u.CreatedAt = unixTime(created)
		out = append(out, u)
	}
	return out, total, rows.Err()
}

type UserUpdate struct {
	Name     *string
	Role     *string
	Disabled *bool
}

func (s *Store) UpdateUser(ctx context.Context, id int64, upd UserUpdate) (*User, error) {
	sets, args := []string{}, []any{}
	if upd.Name != nil {
		sets, args = append(sets, "name = ?"), append(args, *upd.Name)
	}
	if upd.Role != nil {
		sets, args = append(sets, "role = ?"), append(args, *upd.Role)
	}
	if upd.Disabled != nil {
		sets, args = append(sets, "disabled = ?"), append(args, boolInt(*upd.Disabled))
	}
	if len(sets) > 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, id)...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
		if upd.Disabled != nil && *upd.Disabled {
			s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id)
		}
	}
	return s.GetUser(ctx, id)
}

func (s *Store) SetPassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

// AdjustBalance adds amount (may be negative) to a user's balance and records
// it as an admin adjustment.
func (s *Store) AdjustBalance(ctx context.Context, userID, amount int64, note string) (*User, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET balance = balance + ? WHERE id = ?`, amount, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return insertTransaction(ctx, tx, userID, amount, TxAdjust, note)
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, userID)
}

func (s *Store) TotalBalance(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(balance), 0) FROM users`).Scan(&n)
	return n, err
}

// --- sessions ---

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, ttl time.Duration) error {
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, t+int64(ttl.Seconds()), t)
	return err
}

// SessionUser returns the user owning a live session.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.name, u.password_hash, u.role, u.balance, u.disabled, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now()))
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteOtherSessions logs a user out everywhere except the given session.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keepHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, keepHash)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now())
	return err
}
