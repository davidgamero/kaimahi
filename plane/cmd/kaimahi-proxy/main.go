// kaimahi-proxy is the P4a governance plane: a metering and enforcing LLM
// proxy mounted at kagent's ModelConfig baseUrl seam. Two listeners: the
// data plane (governed OpenAI-compatible traffic) and the admin plane
// (credentials, budgets, ledger) on a port the data Service never exposes.
//
// Secrets reach the process only as mounted files (never argv or env
// values); non-secret wiring is env. Migrations run at startup —
// idempotent, so a rollout is its own migration step.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gambtho/kaimahi/plane/internal/config"
	"github.com/gambtho/kaimahi/plane/internal/db"
	"github.com/gambtho/kaimahi/plane/internal/meter"
	"github.com/gambtho/kaimahi/plane/internal/proxy"
	"github.com/gambtho/kaimahi/plane/internal/redact"
	"github.com/gambtho/kaimahi/plane/internal/store"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustReadSecretFile(path, what string) string {
	raw, err := os.ReadFile(path)
	v := strings.TrimSpace(string(raw))
	if err != nil || v == "" {
		slog.Error("missing required secret file", "what", what, "path", path, "err", err)
		os.Exit(1)
	}
	return v
}

func main() {
	dataAddr := env("DATA_ADDR", ":8080")
	adminAddr := env("ADMIN_ADDR", ":9091")
	configFile := env("CONFIG_FILE", "/etc/kaimahi/upstreams.json")
	adminTokenFile := env("ADMIN_TOKEN_FILE", "/etc/kaimahi/admin/token")
	pgPasswordFile := env("PGPASSWORD_FILE", "/etc/kaimahi/pg/password")

	pgPassword := mustReadSecretFile(pgPasswordFile, "postgres password")
	adminToken := mustReadSecretFile(adminTokenFile, "admin token")

	cfg, err := config.Load(configFile)
	if err != nil {
		slog.Error("loading upstream config", "err", err)
		os.Exit(1)
	}

	// Redacting logger: defense in depth — nothing logs secrets on
	// purpose; this catches accidents. Values known at boot only; a
	// rotated upstream credential regains redaction on the next rollout.
	secrets := []string{pgPassword, adminToken}
	for _, u := range cfg.Upstreams {
		if u.CredentialFile == "" {
			continue
		}
		if raw, err := os.ReadFile(u.CredentialFile); err == nil {
			secrets = append(secrets, strings.TrimSpace(string(raw)))
		}
	}
	slog.SetDefault(slog.New(redact.Handler{
		Inner: slog.NewTextHandler(os.Stderr, nil),
		R:     redact.New(secrets),
	}))

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		url.QueryEscape(env("PGUSER", "kaimahi")), url.QueryEscape(pgPassword),
		env("PGHOST", "kaimahi-postgres"), env("PGPORT", "5432"),
		url.QueryEscape(env("PGDATABASE", "kaimahi")))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// Postgres may still be starting alongside us; retry rather than
	// crash-loop through image pulls.
	var pool = retryConnect(ctx, dsn)
	if pool == nil {
		os.Exit(1)
	}
	defer pool.Close()

	st := store.New(pool)
	deps := proxy.Deps{
		Store:  st,
		Meter:  &meter.Meter{Usage: st},
		Config: cfg,
	}

	dataSrv := &http.Server{Addr: dataAddr, Handler: proxy.NewDataMux(deps), ReadHeaderTimeout: 10 * time.Second}
	adminSrv := &http.Server{Addr: adminAddr, Handler: proxy.NewAdminMux(deps, adminTokenFile), ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 2)
	go func() { errCh <- dataSrv.ListenAndServe() }()
	go func() { errCh <- adminSrv.ListenAndServe() }()
	slog.Info("kaimahi-proxy up", "data", dataAddr, "admin", adminAddr,
		"upstreams", len(cfg.Upstreams))

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = dataSrv.Shutdown(shutdownCtx)
		_ = adminSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}
}

func retryConnect(ctx context.Context, dsn string) *pgxpool.Pool {
	deadline := time.Now().Add(90 * time.Second)
	for {
		if err := db.Migrate(ctx, dsn); err != nil {
			if time.Now().After(deadline) || ctx.Err() != nil {
				slog.Error("migrations failed", "err", err)
				return nil
			}
			slog.Warn("waiting for postgres", "err", err)
			select {
			case <-time.After(3 * time.Second):
				continue
			case <-ctx.Done():
				return nil
			}
		}
		pool, err := db.NewPool(ctx, dsn)
		if err != nil {
			slog.Error("connecting pool after migrate", "err", err)
			return nil
		}
		return pool
	}
}
