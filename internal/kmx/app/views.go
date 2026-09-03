package app

import (
	"fmt"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/admin"
)

// The read-only views of the plane: what it has spent, what has been
// granted, and what the enforcement points decided.
//
// UNGUARDED, exactly like `make ledger` and the audit targets. The line the
// guard draws is not "mutates" but "can be aimed somewhere unintended", and
// these reach the cluster only through kubectl carrying an explicit
// --context: they land wherever the rest of the invocation was already going
// to land, and they change nothing when they get there.

// Ledger prints the spend ledger and the month-to-date totals.
func (a *App) Ledger(credential string) error {
	return a.session(func(c *admin.Client) error { return c.Ledger(a.Out, credential) })
}

// Grants lists grants with liveness — an expired grant is not a grant.
func (a *App) Grants(credential string) error {
	return a.session(func(c *admin.Client) error { return c.Grants(a.Out, credential) })
}

// Audit prints one of the plane's audit trails.
func (a *App) Audit(kind, credential string) error {
	switch kind {
	case "tool":
		return a.session(func(c *admin.Client) error { return c.ToolAudit(a.Out, credential) })
	case "approval":
		return a.session(func(c *admin.Client) error { return c.ApprovalAudit(a.Out, credential) })
	default:
		return fmt.Errorf("usage: kmx audit tool|approval [<credential>]")
	}
}

// session opens an admin session for one command and closes it again. Each
// command is its own port-forward: kmx is a CLI, not a daemon, and a forward
// that outlived its command would be exactly the stale forward the plumbing
// refuses to talk through.
func (a *App) session(do func(*admin.Client) error) error {
	client, err := admin.Open(a, a.Cfg.AdminPort, a.Err)
	if err != nil {
		return err
	}
	defer client.Close()
	return do(client)
}
