package store

// P4c approvals: pending requests, bounded grants, and the approvals'
// own audit trail. Everything decision-relevant is evaluated in SQL at
// call time — liveness (expiry, use count) is part of every consuming
// or listing query, so an expired grant is simply not a grant and no
// reaper is needed for correctness.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotPending marks a decision attempted on a request that is no
// longer pending (already approved or denied).
var ErrNotPending = errors.New("store: request is not pending")

// ErrBounds marks an approval whose bounds are invalid for the request:
// no bound at all (an unbounded grant is a config change, not an
// approval), or an amount mismatched to the kind.
var ErrBounds = errors.New("store: invalid grant bounds")

type ApprovalRequest struct {
	ID             string     `json:"id"`
	CredentialName string     `json:"credential"`
	Kind           string     `json:"kind"`
	Subject        string     `json:"subject"`
	Status         string     `json:"status"`
	Detail         string     `json:"detail"`
	CreatedAt      time.Time  `json:"created_at"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
}

// Grant is one bounded permit. At least one of ExpiresAt/MaxUses is
// always set (schema CHECK); Amount is set exactly on budget grants.
type Grant struct {
	ID             string     `json:"id"`
	RequestID      string     `json:"request_id"`
	CredentialName string     `json:"credential"`
	Kind           string     `json:"kind"`
	Subject        string     `json:"subject"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	MaxUses        *int32     `json:"max_uses,omitempty"`
	Uses           int32      `json:"uses"`
	Amount         *int64     `json:"amount,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type ApprovalAuditEntry struct {
	RequestID      string    `json:"request_id"`
	CredentialName string    `json:"credential"`
	Kind           string    `json:"kind"`
	Subject        string    `json:"subject"`
	Action         string    `json:"action"`
	Bounds         string    `json:"bounds"`
	CreatedAt      time.Time `json:"created_at"`
}

// grantLive is the one liveness predicate every consuming and listing
// query shares: not expired, not exhausted — evaluated by Postgres at
// call time, never from a cached copy.
const grantLive = `(expires_at IS NULL OR expires_at > now())
	AND (max_uses IS NULL OR uses < max_uses)`

// FileApprovalRequest files a pending request, deduplicated per
// (credential, kind, subject) among pending rows: refiling while one is
// pending is a no-op (filed=false). A fresh filing also writes the
// 'requested' audit row in the same transaction.
func (s *Store) FileApprovalRequest(ctx context.Context, credential, kind, subject, detail string) (filed bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO approval_request (credential_name, kind, subject, detail)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (credential_name, kind, subject) WHERE status = 'pending' DO NOTHING
		 RETURNING id`,
		credential, kind, subject, detail).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // already pending — deduped
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		return false, fmt.Errorf("%w: no such credential", ErrNotFound)
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO approval_audit (request_id, credential_name, kind, subject, action)
		 VALUES ($1, $2, $3, $4, 'requested')`,
		id, credential, kind, subject); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// PendingApprovals lists pending requests, oldest first (the queue).
func (s *Store) PendingApprovals(ctx context.Context) ([]ApprovalRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, credential_name, kind, subject, status, detail, created_at, decided_at
		 FROM approval_request WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return scanRequests(rows)
}

func scanRequests(rows pgx.Rows) ([]ApprovalRequest, error) {
	defer rows.Close()
	var out []ApprovalRequest
	for rows.Next() {
		var r ApprovalRequest
		if err := rows.Scan(&r.ID, &r.CredentialName, &r.Kind, &r.Subject,
			&r.Status, &r.Detail, &r.CreatedAt, &r.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApproveRequest decides a pending request and mints its bounded grant
// atomically: grant insert, request status flip, and the 'approved'
// audit row commit together or not at all — a failed audit write fails
// the approval (fail closed, stronger than the breaker contract).
// Bound validation (at least one of expiresAt/maxUses; amount exactly
// on budget kinds) is the caller's job and the schema's backstop.
func (s *Store) ApproveRequest(ctx context.Context, id string,
	expiresAt *time.Time, maxUses *int32, amount *int64) (Grant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var r ApprovalRequest
	err = tx.QueryRow(ctx,
		`SELECT id, credential_name, kind, subject, status
		 FROM approval_request WHERE id = $1 FOR UPDATE`,
		id).Scan(&r.ID, &r.CredentialName, &r.Kind, &r.Subject, &r.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, err
	}
	if r.Status != "pending" {
		return Grant{}, ErrNotPending
	}
	// Ported permit discipline: a grant allowing everything forever is
	// an error, not a wide grant — at least one bound REQUIRED; budget
	// grants carry exactly one amount, tool grants none.
	if expiresAt == nil && maxUses == nil {
		return Grant{}, fmt.Errorf("%w: at least one of TTL and USES is required", ErrBounds)
	}
	if (r.Kind == "budget") != (amount != nil) {
		return Grant{}, fmt.Errorf("%w: AMOUNT is required for budget grants and forbidden otherwise", ErrBounds)
	}

	g := Grant{RequestID: r.ID, CredentialName: r.CredentialName,
		Kind: r.Kind, Subject: r.Subject, ExpiresAt: expiresAt, MaxUses: maxUses, Amount: amount}
	if err := tx.QueryRow(ctx,
		`INSERT INTO permit_grant (request_id, credential_name, kind, subject, expires_at, max_uses, amount)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		r.ID, r.CredentialName, r.Kind, r.Subject, expiresAt, maxUses, amount).Scan(&g.ID, &g.CreatedAt); err != nil {
		return Grant{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE approval_request SET status = 'approved', decided_at = now() WHERE id = $1`, r.ID); err != nil {
		return Grant{}, err
	}
	bounds := ""
	if expiresAt != nil {
		bounds += fmt.Sprintf("expires=%s ", expiresAt.UTC().Format(time.RFC3339))
	}
	if maxUses != nil {
		bounds += fmt.Sprintf("uses=%d ", *maxUses)
	}
	if amount != nil {
		bounds += fmt.Sprintf("amount=%d ", *amount)
	}
	bounds = strings.TrimSpace(bounds)
	if _, err := tx.Exec(ctx,
		`INSERT INTO approval_audit (request_id, credential_name, kind, subject, action, bounds)
		 VALUES ($1, $2, $3, $4, 'approved', $5)`,
		r.ID, r.CredentialName, r.Kind, r.Subject, bounds); err != nil {
		return Grant{}, err
	}
	return g, tx.Commit(ctx)
}

// DenyApprovalRequest decides a pending request negatively; the request
// stays on record (denied), and a fresh denial can file a new one.
func (s *Store) DenyApprovalRequest(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var r ApprovalRequest
	err = tx.QueryRow(ctx,
		`SELECT id, credential_name, kind, subject, status
		 FROM approval_request WHERE id = $1 FOR UPDATE`,
		id).Scan(&r.ID, &r.CredentialName, &r.Kind, &r.Subject, &r.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if r.Status != "pending" {
		return ErrNotPending
	}
	if _, err := tx.Exec(ctx,
		`UPDATE approval_request SET status = 'denied', decided_at = now() WHERE id = $1`, r.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO approval_audit (request_id, credential_name, kind, subject, action)
		 VALUES ($1, $2, $3, $4, 'denied')`,
		r.ID, r.CredentialName, r.Kind, r.Subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConsumeToolGrant admits one tool call under a live grant, consuming
// one use atomically. FOR UPDATE SKIP LOCKED makes concurrent consumers
// skip a row another transaction holds — the loser denies (a spurious
// denial under contention, never a double-spent use; the liveness
// predicate is evaluated on the locked row). ok=false means no
// consumable grant — the caller denies.
func (s *Store) ConsumeToolGrant(ctx context.Context, credential, tool string) (grantID string, ok bool, err error) {
	err = s.pool.QueryRow(ctx,
		`UPDATE permit_grant SET uses = uses + 1
		 WHERE id = (
		   SELECT id FROM permit_grant
		   WHERE credential_name = $1 AND kind = 'tool' AND subject = $2 AND `+grantLive+`
		   ORDER BY created_at LIMIT 1
		   FOR UPDATE SKIP LOCKED
		 ) RETURNING id`,
		credential, tool).Scan(&grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return grantID, true, nil
}

// LiveToolGrantSubjects lists the tools a credential can currently call
// via grants — for the tools/list projection (read-only: listing burns
// no uses).
func (s *Store) LiveToolGrantSubjects(ctx context.Context, credential string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT subject FROM permit_grant
		 WHERE credential_name = $1 AND kind = 'tool' AND `+grantLive+`
		 ORDER BY subject`, credential)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// BudgetNeed is one exceeded cap a request must cover via grants.
type BudgetNeed struct {
	Subject string // "tokens" or "cents"
	Used    int64
	Cap     int64
}

// ConsumeBudgetGrants admits one over-cap request when live budget
// grants cover EVERY exceeded cap: per need, admitted iff
// used < cap + SUM(live amounts), consuming one use from the oldest
// live grant — all needs in ONE transaction, so a request denied on any
// cap burns no uses on the others. failedSubject names the first
// uncovered cap ("" = admitted).
func (s *Store) ConsumeBudgetGrants(ctx context.Context, credential string, needs []BudgetNeed) (failedSubject string, err error) {
	if len(needs) == 0 {
		return "", nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return needs[0].Subject, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, n := range needs {
		var extra int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(SUM(amount), 0) FROM permit_grant
			 WHERE credential_name = $1 AND kind = 'budget' AND subject = $2 AND `+grantLive,
			credential, n.Subject).Scan(&extra); err != nil {
			return n.Subject, err
		}
		if extra <= 0 || n.Used >= n.Cap+extra {
			return n.Subject, nil
		}
		var grantID string
		err = tx.QueryRow(ctx,
			`UPDATE permit_grant SET uses = uses + 1
			 WHERE id = (
			   SELECT id FROM permit_grant
			   WHERE credential_name = $1 AND kind = 'budget' AND subject = $2 AND `+grantLive+`
			   ORDER BY created_at LIMIT 1
			   FOR UPDATE SKIP LOCKED
			 ) RETURNING id`,
			credential, n.Subject).Scan(&grantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return n.Subject, nil // raced away between SUM and consume — deny
		}
		if err != nil {
			return n.Subject, err
		}
	}
	return "", tx.Commit(ctx)
}

// Grants lists a credential's grants (all credentials when empty),
// newest first, with liveness computed by the same predicate consumers
// use.
func (s *Store) Grants(ctx context.Context, credential string, limit int) ([]Grant, []bool, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, request_id, credential_name, kind, subject, expires_at, max_uses, uses, amount, created_at,
		        `+grantLive+` AS live
		 FROM permit_grant
		 WHERE ($1 = '' OR credential_name = $1)
		 ORDER BY created_at DESC LIMIT $2`,
		credential, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Grant
	var live []bool
	for rows.Next() {
		var g Grant
		var l bool
		if err := rows.Scan(&g.ID, &g.RequestID, &g.CredentialName, &g.Kind, &g.Subject,
			&g.ExpiresAt, &g.MaxUses, &g.Uses, &g.Amount, &g.CreatedAt, &l); err != nil {
			return nil, nil, err
		}
		out = append(out, g)
		live = append(live, l)
	}
	return out, live, rows.Err()
}

// ApprovalAudit returns the newest approval-trail rows for one
// credential (all when empty), newest first.
func (s *Store) ApprovalAudit(ctx context.Context, credential string, limit int) ([]ApprovalAuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT request_id, credential_name, kind, subject, action, bounds, created_at
		 FROM approval_audit
		 WHERE ($1 = '' OR credential_name = $1)
		 ORDER BY created_at DESC LIMIT $2`,
		credential, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovalAuditEntry
	for rows.Next() {
		var e ApprovalAuditEntry
		if err := rows.Scan(&e.RequestID, &e.CredentialName, &e.Kind, &e.Subject,
			&e.Action, &e.Bounds, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
