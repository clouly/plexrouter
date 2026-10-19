// Package store is the persistence layer (SQLite via a pure-Go driver).
//
// Money is stored as integer nano-dollars (1 USD = 1e9) so that per-request
// costs of fractions of a cent add up exactly.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

const nanoPerUSD = 1e9

func ToUSD(nano int64) float64  { return float64(nano) / nanoPerUSD }
func FromUSD(usd float64) int64 { return int64(math.Round(usd * nanoPerUSD)) }

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping checks that the database is usable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// migrations are applied in order; PRAGMA user_version records how many ran.
// Never edit a released migration — append a new one.
var migrations = []string{`
CREATE TABLE users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	name          TEXT    NOT NULL DEFAULT '',
	password_hash TEXT    NOT NULL,
	role          TEXT    NOT NULL DEFAULT 'user',
	balance       INTEGER NOT NULL DEFAULT 0,
	disabled      INTEGER NOT NULL DEFAULT 0,
	created_at    INTEGER NOT NULL
);
CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE TABLE api_keys (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT    NOT NULL DEFAULT '',
	key_hash     TEXT    NOT NULL UNIQUE,
	prefix       TEXT    NOT NULL,
	credit_limit INTEGER,
	usage        INTEGER NOT NULL DEFAULT 0,
	disabled     INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER
);
CREATE INDEX api_keys_user ON api_keys(user_id);
CREATE TABLE providers (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT    NOT NULL,
	type        TEXT    NOT NULL,
	base_url    TEXT    NOT NULL DEFAULT '',
	api_key_enc TEXT    NOT NULL DEFAULT '',
	enabled     INTEGER NOT NULL DEFAULT 1,
	created_at  INTEGER NOT NULL
);
CREATE TABLE models (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	slug           TEXT    NOT NULL UNIQUE,
	name           TEXT    NOT NULL DEFAULT '',
	description    TEXT    NOT NULL DEFAULT '',
	context_length INTEGER NOT NULL DEFAULT 0,
	input_price    REAL    NOT NULL DEFAULT 0,
	output_price   REAL    NOT NULL DEFAULT 0,
	enabled        INTEGER NOT NULL DEFAULT 1,
	created_at     INTEGER NOT NULL
);
CREATE TABLE model_endpoints (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	model_id       INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
	provider_id    INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
	upstream_model TEXT    NOT NULL,
	priority       INTEGER NOT NULL DEFAULT 0,
	enabled        INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX model_endpoints_model ON model_endpoints(model_id);
CREATE TABLE request_logs (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id           INTEGER NOT NULL,
	api_key_id        INTEGER NOT NULL,
	api_key_name      TEXT    NOT NULL DEFAULT '',
	model             TEXT    NOT NULL,
	provider_id       INTEGER NOT NULL DEFAULT 0,
	provider_name     TEXT    NOT NULL DEFAULT '',
	prompt_tokens     INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	cost              INTEGER NOT NULL DEFAULT 0,
	latency_ms        INTEGER NOT NULL DEFAULT 0,
	status            TEXT    NOT NULL,
	error             TEXT    NOT NULL DEFAULT '',
	stream            INTEGER NOT NULL DEFAULT 0,
	created_at        INTEGER NOT NULL
);
CREATE INDEX request_logs_user_time ON request_logs(user_id, created_at);
CREATE INDEX request_logs_time ON request_logs(created_at);
CREATE TABLE transactions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	amount     INTEGER NOT NULL,
	type       TEXT    NOT NULL,
	note       TEXT    NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX transactions_user ON transactions(user_id, created_at);
CREATE TABLE redeem_codes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	code       TEXT    NOT NULL UNIQUE,
	amount     INTEGER NOT NULL,
	used_by    INTEGER REFERENCES users(id) ON DELETE SET NULL,
	used_at    INTEGER,
	created_at INTEGER NOT NULL
);
`, `
CREATE TABLE user_identities (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider   TEXT    NOT NULL,
	subject    TEXT    NOT NULL,
	email      TEXT    NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	UNIQUE (provider, subject),
	UNIQUE (user_id, provider)
);
`, `
CREATE TABLE provider_keys (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	provider_id   INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
	name          TEXT    NOT NULL DEFAULT '',
	key_enc       TEXT    NOT NULL,
	enabled       INTEGER NOT NULL DEFAULT 1,
	status        TEXT    NOT NULL DEFAULT 'active',
	last_error    TEXT    NOT NULL DEFAULT '',
	last_error_at INTEGER,
	last_used_at  INTEGER,
	created_at    INTEGER NOT NULL
);
CREATE INDEX provider_keys_provider ON provider_keys(provider_id);
INSERT INTO provider_keys (provider_id, name, key_enc, created_at)
	SELECT id, 'Default', api_key_enc, created_at FROM providers WHERE api_key_enc != '';
UPDATE providers SET api_key_enc = '';

ALTER TABLE model_endpoints ADD COLUMN input_cost REAL;
ALTER TABLE model_endpoints ADD COLUMN output_cost REAL;

ALTER TABLE request_logs ADD COLUMN upstream_cost INTEGER;
ALTER TABLE request_logs ADD COLUMN provider_key_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN provider_key_name TEXT NOT NULL DEFAULT '';
CREATE INDEX request_logs_provider_time ON request_logs(provider_id, created_at);

ALTER TABLE api_keys ADD COLUMN rate_limit_rpm INTEGER;
ALTER TABLE api_keys ADD COLUMN allowed_models TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN rate_limit_rpm INTEGER;
`, `
ALTER TABLE models ADD COLUMN cache_read_price REAL;
ALTER TABLE models ADD COLUMN cache_write_price REAL;
ALTER TABLE model_endpoints ADD COLUMN cache_read_cost REAL;
ALTER TABLE model_endpoints ADD COLUMN cache_write_cost REAL;
ALTER TABLE request_logs ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN api TEXT NOT NULL DEFAULT 'chat';
`}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Page is a 1-based pagination request.
type Page struct {
	Page, Size int
}

func (p Page) limitOffset() (int, int) {
	size := p.Size
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	page := max(p.Page, 1)
	return size, (page - 1) * size
}

func now() int64 { return time.Now().Unix() }

func unixTime(v int64) time.Time { return time.Unix(v, 0).UTC() }

func nullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := unixTime(v.Int64)
	return &t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
