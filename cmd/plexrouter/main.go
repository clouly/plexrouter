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
	"strings"
	"syscall"
	"time"

	"github.com/local4a/plexrouter/internal/config"
	"github.com/local4a/plexrouter/internal/gateway"
	"github.com/local4a/plexrouter/internal/secure"
	"github.com/local4a/plexrouter/internal/server"
	"github.com/local4a/plexrouter/internal/store"
	"github.com/local4a/plexrouter/web"
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

	box, err := secure.NewBox(cfg.Secret)
	if err != nil {
		return err
	}
	gw := gateway.New(st, box, gateway.Options{Timeout: cfg.RequestTimeout, DefaultUserRPM: cfg.DefaultUserRPM}, log)
	srv := server.New(cfg, st, box, gw, web.Dist(), version, log)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go housekeeping(ctx, st, srv.LogRetentionDays)

	errc := make(chan error, 1)
	go func() {
		log.Info("PlexRouter listening", "addr", cfg.Addr, "version", version, "db", cfg.DatabasePath)
		if hasAdmin, err := st.HasAdmin(ctx); err == nil && !hasAdmin {
			port := cfg.Addr[strings.LastIndexByte(cfg.Addr, ':')+1:]
			log.Info("first run: open the console in your browser to finish setup", "url", "http://localhost:"+port)
		}
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

// housekeeping hourly removes expired sessions and request logs older than
// the retention period (retentionDays 0 keeps logs forever).
func housekeeping(ctx context.Context, st *store.Store, retention func() int) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := st.PurgeExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("purge sessions", "err", err)
		}
		if retentionDays := retention(); retentionDays > 0 {
			n, err := st.PurgeLogs(ctx, time.Now().AddDate(0, 0, -retentionDays))
			if err != nil && ctx.Err() == nil {
				slog.Warn("purge request logs", "err", err)
			} else if n > 0 {
				slog.Info("purged old request logs", "rows", n, "retention_days", retentionDays)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
