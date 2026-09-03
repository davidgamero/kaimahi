// Package store is the plane's Postgres layer: credentials (hashes only),
// budget caps, and the append-only spend ledger. Rewritten for the kagent
// architecture — tomte-old's store carried the replaced control plane
// (tenants, runs, workflows); only its spend-ledger pattern survives here.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrExists   = errors.New("store: already exists")
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Credential is a Kaimahi-issued identity: the opaque token the governed
// preset carries, known here only by its sha256. Caps are monthly (UTC);
// nil means no cap of that kind.
type Credential struct {
	Name      string
	CapCents  *int64
	CapTokens *int64
}

// LedgerEntry is one append-only spend row. CostSource records why the
// cost is what it is ('free', 'priced', 'unpriced', 'denied') — a zero
// cost always carries its explanation (no blanket $0).
type LedgerEntry struct {
	CredentialName string    `json:"credential"`
	Upstream       string    `json:"upstream"`
	Model          string    `json:"model"`
	InputTokens    int64     `json:"input_tokens"`
	OutputTokens   int64     `json:"output_tokens"`
	CostCents      int64     `json:"cost_cents"`
	CostSource     string    `json:"cost_source"`
	Status         int       `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Store) CreateCredential(ctx context.Context, name string, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO credential (name, token_hash) VALUES ($1, $2)`, name, tokenHash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ErrExists
	}
	return err
}

// CredentialByTokenHash resolves an inbound bearer to its credential.
// ErrNotFound means an unknown token — the caller answers 401.
func (s *Store) CredentialByTokenHash(ctx context.Context, tokenHash []byte) (Credential, error) {
	var c Credential
	err := s.pool.QueryRow(ctx,
		`SELECT name, cap_cents, cap_tokens FROM credential WHERE token_hash = $1`,
		tokenHash).Scan(&c.Name, &c.CapCents, &c.CapTokens)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	return c, err
}

// SetBudget replaces both caps for the named credential (nil clears a cap).
func (s *Store) SetBudget(ctx context.Context, name string, capCents, capTokens *int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE credential SET cap_cents = $2, cap_tokens = $3 WHERE name = $1`,
		name, capCents, capTokens)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordLedger appends one spend row and, in the same transaction,
// consumes the reservation the call was admitted under (P9: the row
// replaces the hold; empty when the call held nothing — a denial, or a
// credential with no caps). The ledger stays append-only: the one
// delete here is of a reservation, never of a row. A reservation that
// already expired and was swept is simply gone; the row still lands.
func (s *Store) RecordLedger(ctx context.Context, e LedgerEntry, reservationID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entry (credential_name, upstream, model, input_tokens, output_tokens, cost_cents, cost_source, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.CredentialName, e.Upstream, e.Model, e.InputTokens, e.OutputTokens, e.CostCents, e.CostSource, e.Status); err != nil {
		return err
	}
	if reservationID != "" {
		if _, err := tx.Exec(ctx,
			`DELETE FROM spend_reservation WHERE id = $1`, reservationID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ToolAuditEntry is one append-only tool-governance row: what the MCP
// gateway decided about one inbound method. Decision says whose status
// the row carries: 'allowed' rows record the upstream's HTTP status,
// 'denied' rows the gateway's own (like P4a's denied ledger rows).
type ToolAuditEntry struct {
	CredentialName string `json:"credential"`
	Upstream       string `json:"upstream"`
	Method         string `json:"method"`
	Tool           string `json:"tool"`
	Decision       string `json:"decision"`
	Status         int    `json:"status"`
	Detail         string `json:"detail"`
	// ArgDigest/ArgSummary (P12) identify the CALL: the digest an
	// approval is welded to, and the transaction line built from the
	// tool's declared policy fields. Present on tools/call rows —
	// denied and allowed alike, so the approved call and the call that
	// ran are provably the same one — and empty on other methods.
	// Arbitrary arguments are never recorded: this table is in every
	// pg_dump (`make backup`).
	ArgDigest  string    `json:"arg_digest,omitempty"`
	ArgSummary string    `json:"arg_summary,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SetToolAllowlist replaces the credential's whole allowlist (empty
// clears it — nothing callable). Transactional so a reader never sees a
// half-replaced set.
func (s *Store) SetToolAllowlist(ctx context.Context, credentialName string, tools []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM credential WHERE name = $1)`, credentialName).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM tool_allowlist WHERE credential_name = $1`, credentialName); err != nil {
		return err
	}
	for _, tool := range tools {
		if _, err := tx.Exec(ctx,
			`INSERT INTO tool_allowlist (credential_name, tool) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			credentialName, tool); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ToolAllowlist returns the credential's allowed tools, sorted. An empty
// result is meaningful: nothing callable.
func (s *Store) ToolAllowlist(ctx context.Context, credentialName string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tool FROM tool_allowlist WHERE credential_name = $1 ORDER BY tool`, credentialName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RecordToolAudit appends one tool-audit row. Append-only by
// construction, like the spend ledger.
func (s *Store) RecordToolAudit(ctx context.Context, e ToolAuditEntry) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tool_audit (credential_name, upstream, method, tool, decision, status, detail, arg_digest, arg_summary)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.CredentialName, e.Upstream, e.Method, e.Tool, e.Decision, e.Status, e.Detail, e.ArgDigest, e.ArgSummary)
	return err
}

// ToolAudit returns the newest audit rows for one credential (all
// credentials when name is empty), newest first.
func (s *Store) ToolAudit(ctx context.Context, credentialName string, limit int) ([]ToolAuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT credential_name, upstream, method, tool, decision, status, detail, arg_digest, arg_summary, created_at
		 FROM tool_audit
		 WHERE ($1 = '' OR credential_name = $1)
		 ORDER BY created_at DESC LIMIT $2`,
		credentialName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolAuditEntry
	for rows.Next() {
		var e ToolAuditEntry
		if err := rows.Scan(&e.CredentialName, &e.Upstream, &e.Method, &e.Tool,
			&e.Decision, &e.Status, &e.Detail, &e.ArgDigest, &e.ArgSummary, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MonthUsage sums the ledger for one credential since monthStart. Denied
// rows carry zero usage so including them is harmless; billed usage counts
// whether or not the surrounding request succeeded (spend is recorded
// before failures are honored — standing guidance).
func (s *Store) MonthUsage(ctx context.Context, credentialName string, monthStart time.Time) (cents, tokens int64, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_cents), 0), COALESCE(SUM(input_tokens + output_tokens), 0)
		 FROM ledger_entry WHERE credential_name = $1 AND created_at >= $2`,
		credentialName, monthStart).Scan(&cents, &tokens)
	return cents, tokens, err
}

// Ledger returns the newest entries for one credential (all credentials
// when name is empty), newest first.
func (s *Store) Ledger(ctx context.Context, credentialName string, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT credential_name, upstream, model, input_tokens, output_tokens, cost_cents, cost_source, status, created_at
		 FROM ledger_entry
		 WHERE ($1 = '' OR credential_name = $1)
		 ORDER BY created_at DESC LIMIT $2`,
		credentialName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.CredentialName, &e.Upstream, &e.Model, &e.InputTokens,
			&e.OutputTokens, &e.CostCents, &e.CostSource, &e.Status, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
