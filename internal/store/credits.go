package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	TxSignup = "signup"
	TxRedeem = "redeem"
	TxAdjust = "adjust"
)

type Transaction struct {
	ID        int64
	UserID    int64
	Amount    int64 // nano-USD, positive = credit
	Type      string
	Note      string
	CreatedAt time.Time
}

func insertTransaction(ctx context.Context, tx *sql.Tx, userID, amount int64, typ, note string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO transactions (user_id, amount, type, note, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, amount, typ, note, now())
	return err
}

func (s *Store) ListTransactions(ctx context.Context, userID int64, p Page) ([]Transaction, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, amount, type, note, created_at FROM transactions
		WHERE user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Transaction{}
	for rows.Next() {
		var t Transaction
		var created int64
		if err := rows.Scan(&t.ID, &t.UserID, &t.Amount, &t.Type, &t.Note, &created); err != nil {
			return nil, 0, err
		}
		t.CreatedAt = unixTime(created)
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// --- redeem codes ---

type RedeemCode struct {
	ID          int64
	Code        string
	Amount      int64
	UsedByEmail *string
	UsedAt      *time.Time
	CreatedAt   time.Time
}

var ErrCodeInvalid = errors.New("invalid or already used code")

func (s *Store) CreateRedeemCodes(ctx context.Context, codes []string, amount int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, c := range codes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO redeem_codes (code, amount, created_at) VALUES (?, ?, ?)`, c, amount, now()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListRedeemCodes(ctx context.Context, p Page) ([]RedeemCode, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM redeem_codes`).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.code, c.amount, u.email, c.used_at, c.created_at
		FROM redeem_codes c LEFT JOIN users u ON u.id = c.used_by ORDER BY c.id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []RedeemCode{}
	for rows.Next() {
		var c RedeemCode
		var email sql.NullString
		var usedAt sql.NullInt64
		var created int64
		if err := rows.Scan(&c.ID, &c.Code, &c.Amount, &email, &usedAt, &created); err != nil {
			return nil, 0, err
		}
		if email.Valid {
			c.UsedByEmail = &email.String
		}
		c.UsedAt = nullTime(usedAt)
		c.CreatedAt = unixTime(created)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (s *Store) DeleteRedeemCode(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM redeem_codes WHERE id = ? AND used_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Redeem marks a code used and credits its amount; it returns the amount and new balance.
func (s *Store) Redeem(ctx context.Context, userID int64, code string) (amount, balance int64, err error) {
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id, amount FROM redeem_codes WHERE code = ? AND used_at IS NULL`, code).Scan(&id, &amount)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCodeInvalid
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE redeem_codes SET used_by = ?, used_at = ? WHERE id = ?`, userID, now(), id); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `UPDATE users SET balance = balance + ? WHERE id = ? RETURNING balance`, amount, userID).Scan(&balance); err != nil {
			return err
		}
		return insertTransaction(ctx, tx, userID, amount, TxRedeem, "Redeemed code "+maskCode(code))
	})
	return amount, balance, err
}

func maskCode(c string) string {
	if len(c) <= 8 {
		return c
	}
	return c[:4] + "…" + c[len(c)-4:]
}
