// kaimahi-proxy is the Kaimahi governance plane: the P4a metering and
// enforcing LLM proxy mounted at kagent's ModelConfig baseUrl seam, and
// the P4b enforcing MCP gateway mounted at the tool-server seam. Three
// listeners: the LLM data plane, the MCP gateway (own Service), and the
// admin plane (credentials, budgets, allowlists, ledger, audit) on a
// port no data Service exposes.
//
// Secrets reach the process only as mounted files (never argv or env
// values); non-secret wiring is env. Migrations run at startup —
// idempotent, so a rollout is its own migration step.
package main

import (
	"context"
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
	"github.com/gambtho/kaimahi/plane/internal/gateway"
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
	mcpAddr := env("MCP_ADDR", ":8081")
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
		} else {
			// Not fatal (the upstream is optional and read per request),
			// but say so: this credential is outside the redactor until
			// the next rollout.
			slog.Warn("upstream credential unreadable at boot; value not redacted in logs",
				"file", u.CredentialFile, "err", err)
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

	// ReadTimeout bounds slow request-body writers (chat requests are
	// small; streamed RESPONSES are unaffected — WriteTimeout stays 0 so
	// long generations can flush indefinitely).
	// The P4b MCP gateway shares this process (and its pool, redactor,
	// and fail-closed machinery); its listener gets its own Service so
	// the tool seam has its own address.
	gwDeps := gateway.Deps{Store: st, Upstreams: cfg.ToolUpstreams}

	dataSrv := &http.Server{Addr: dataAddr, Handler: proxy.NewDataMux(deps),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	mcpSrv := &http.Server{Addr: mcpAddr, Handler: gateway.NewMux(gwDeps),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	adminSrv := &http.Server{Addr: adminAddr, Handler: proxy.NewAdminMux(deps, adminTokenFile),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}

	errCh := make(chan error, 3)
	go func() { errCh <- dataSrv.ListenAndServe() }()
	go func() { errCh <- mcpSrv.ListenAndServe() }()
	go func() { errCh <- adminSrv.ListenAndServe() }()
	slog.Info("kaimahi-proxy up", "data", dataAddr, "mcp", mcpAddr, "admin", adminAddr,
		"upstreams", len(cfg.Upstreams), "tool_upstreams", len(cfg.ToolUpstreams))

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = dataSrv.Shutdown(shutdownCtx)
		_ = mcpSrv.Shutdown(shutdownCtx)
		_ = adminSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		// Either listener stopping before a shutdown signal is abnormal —
		// even ErrServerClosed — so exit nonzero and let Kubernetes
		// restart the pod rather than report a clean exit.
		slog.Error("server stopped unexpectedly", "err", err)
		os.Exit(1)
	}
}

func retryConnect(ctx context.Context, dsn string) *pgxpool.Pool {
	deadline := time.Now().Add(90 * time.Second)
	for {
		// Bound each attempt — migrate and pool ping together — so a
		// hung connection cannot outlive the retry budget silently. The
		// pool itself is not tied to attemptCtx; it only bounds startup.
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		pool, err := connectOnce(attemptCtx, dsn)
		cancel()
		if err == nil {
			return pool
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			slog.Error("database startup failed", "err", err)
			return nil
		}
		slog.Warn("waiting for postgres", "err", err)
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return nil
		}
	}
}

func connectOnce(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if err := db.Migrate(ctx, dsn); err != nil {
		return nil, err
	}
	return db.NewPool(ctx, dsn)
}
