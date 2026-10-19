// Package config loads server settings from environment variables.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string        // listen address, e.g. ":8080"
	DataDir        string        // directory for the database and generated secret
	DatabasePath   string        // SQLite file path
	Secret         string        // used to encrypt provider API keys at rest
	Name           string        // brand name shown in the console
	AllowRegister  bool          // whether anyone can sign up
	SignupCredit   float64       // USD granted to new accounts
	AdminEmails    []string      // lowercased; these accounts are admins. Empty = first user is admin
	RequestTimeout time.Duration // upper bound for a single upstream request
	TrustProxy     bool          // trust X-Forwarded-For / X-Real-IP (behind a reverse proxy)
	LogLevel       slog.Level

	// PublicURL is the externally visible base URL (https://api.example.com),
	// used to build OAuth callback URLs. Derived from the request if empty.
	PublicURL          string
	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
}

func Load() (*Config, error) {
	c := &Config{
		Addr:           env("PLEXROUTER_ADDR", ":8080"),
		DataDir:        env("PLEXROUTER_DATA_DIR", "./data"),
		Name:           env("PLEXROUTER_NAME", "PlexRouter"),
		AllowRegister:  envBool("PLEXROUTER_ALLOW_REGISTER", true),
		SignupCredit:   envFloat("PLEXROUTER_SIGNUP_CREDIT", 0),
		AdminEmails:    envList("PLEXROUTER_ADMIN_EMAILS"),
		RequestTimeout: envDuration("PLEXROUTER_REQUEST_TIMEOUT", 10*time.Minute),
		TrustProxy:     envBool("PLEXROUTER_TRUST_PROXY", false),

		PublicURL:          strings.TrimRight(os.Getenv("PLEXROUTER_PUBLIC_URL"), "/"),
		GoogleClientID:     os.Getenv("PLEXROUTER_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("PLEXROUTER_GOOGLE_CLIENT_SECRET"),
		GitHubClientID:     os.Getenv("PLEXROUTER_GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("PLEXROUTER_GITHUB_CLIENT_SECRET"),
	}
	if err := c.LogLevel.UnmarshalText([]byte(env("PLEXROUTER_LOG_LEVEL", "info"))); err != nil {
		return nil, fmt.Errorf("PLEXROUTER_LOG_LEVEL: %w", err)
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	c.DatabasePath = env("PLEXROUTER_DB", filepath.Join(c.DataDir, "plexrouter.db"))

	secret, err := loadSecret(c.DataDir)
	if err != nil {
		return nil, err
	}
	c.Secret = secret
	return c, nil
}

// loadSecret returns PLEXROUTER_SECRET, or a random secret persisted in the data
// dir so that a zero-config install still survives restarts.
func loadSecret(dataDir string) (string, error) {
	if s := os.Getenv("PLEXROUTER_SECRET"); s != "" {
		return s, nil
	}
	path := filepath.Join(dataDir, "secret.key")
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(s+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	slog.Info("generated new secret; set PLEXROUTER_SECRET to manage it yourself", "path", path)
	return s, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envList splits a comma-separated variable into lowercased, trimmed items.
func envList(key string) []string {
	var out []string
	for _, v := range strings.Split(os.Getenv(key), ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func envBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}
