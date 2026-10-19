// Command plexrouter runs the PlexRouter server: an OpenAI-compatible LLM
// gateway with a web console, API keys and usage billing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/clouly/plexrouter/internal/config"
	"github.com/clouly/plexrouter/internal/gateway"
	"github.com/clouly/plexrouter/internal/secure"
	"github.com/clouly/plexrouter/internal/server"
	"github.com/clouly/plexrouter/internal/store"
	"github.com/clouly/plexrouter/web"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	if err := bootstrapAdmin(st, cfg); err != nil {
		return err
	}

	box, err := secure.NewBox(cfg.Secret)
	if err != nil {
		return err
	}
	gw := gateway.New(st, box, cfg.RequestTimeout, log)
	srv := server.New(cfg, st, box, gw, web.Dist(), version, log)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go purgeSessions(ctx, st)

	errc := make(chan error, 1)
	go func() {
		log.Info("PlexRouter listening", "addr", cfg.Addr, "version", version, "db", cfg.DatabasePath)
		errc <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
	return nil
}

// bootstrapAdmin creates the admin account from env vars on an empty database.
func bootstrapAdmin(st *store.Store, cfg *config.Config) error {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return nil
	}
	ctx := context.Background()
	n, err := st.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, cfg.AdminEmail, "Admin", string(hash), 0); err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	slog.Info("created admin account", "email", cfg.AdminEmail)
	return nil
}

func purgeSessions(ctx context.Context, st *store.Store) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := st.PurgeExpiredSessions(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("purge sessions", "err", err)
			}
		}
	}
}
