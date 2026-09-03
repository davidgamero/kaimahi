package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

// The read views. Every format string, column width, header and empty-case
// line below is plane-admin.sh's, verbatim — because these are not decorative.
// CI greps them (`hello-world +ollama +qwen2\.5:3b +[0-9]+ +[0-9]+ +0 +free
// +200`), the docs quote them, and an operator reading a ledger from `kmx`
// and one reading it from `make` must see the same table.

const (
	ledgerFmt   = "%-19s %-12s %-9s %-16s %6s %6s %6s %-8s %s\n"
	grantsFmt   = "%-36s %-12s %-8s %-18s %-6s %-22s %-9s %-8s %-19s %-18s %s\n"
	toolFmt     = "%-19s %-12s %-12s %-12s %-24s %-8s %6s %-44s %s\n"
	approvalFmt = "%-19s %-12s %-8s %-18s %-10s %-18s %-40s %s\n"
)

// Ledger prints the spend ledger, newest first, plus month-to-date totals.
func (c *Client) Ledger(out io.Writer, credential string) error {
	doc, err := c.Get("ledger", "/admin/ledger?credential="+url.QueryEscape(credential)+"&limit=50")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, ledgerFmt, "created (UTC)", "credential", "upstream", "model",
		"in", "out", "cents", "source", "status")
	for _, row := range rows(doc, "entries") {
		fmt.Fprintf(out, ledgerFmt,
			trunc(str(row["created_at"]), 19), str(row["credential"]), str(row["upstream"]),
			trunc(str(row["model"]), 16),
			str(row["input_tokens"]), str(row["output_tokens"]), str(row["cost_cents"]),
			str(row["cost_source"]), str(row["status"]))
	}
	if _, ok := doc["month_cents"]; ok {
		fmt.Fprintf(out, "-- month to date: %s cents, %s tokens\n",
			str(doc["month_cents"]), str(doc["month_tokens"]))
	}
	return nil
}

// Grants lists grants with liveness — an expired grant is not a grant.
func (c *Client) Grants(out io.Writer, credential string) error {
	doc, err := c.Get("grants", "/admin/grants?credential="+url.QueryEscape(credential)+"&limit=50")
	if err != nil {
		return err
	}
	list := rows(doc, "grants")
	if len(list) == 0 {
		fmt.Fprintln(out, "no grants")
		return nil
	}
	fmt.Fprintf(out, grantsFmt, "id", "credential", "kind", "subject", "live",
		"expires (UTC)", "uses", "amount", "created (UTC)", "decided by", "binds")
	for _, g := range list {
		uses := str(g["uses"])
		if max, ok := g["max_uses"]; ok && max != nil {
			uses += "/" + str(max)
		}
		fmt.Fprintf(out, grantsFmt,
			str(g["id"]), str(g["credential"]), str(g["kind"]), str(g["subject"]),
			yesno(g["live"]),
			// [:19] inside a 22-wide column, exactly as the script slices
			// it: an expiry is printed to the second, and the extra width
			// is the gap before the next column.
			trunc(dash(g["expires_at"]), 19), uses, dash(g["amount"]),
			trunc(str(g["created_at"]), 19), dash(g["decided_by"]), binds(g))
	}
	return nil
}

// ToolAudit prints the tool-call audit trail, newest first.
func (c *Client) ToolAudit(out io.Writer, credential string) error {
	doc, err := c.Get("tool-audit", "/admin/tool-audit?credential="+url.QueryEscape(credential)+"&limit=50")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, toolFmt, "created (UTC)", "credential", "upstream", "method",
		"tool", "decision", "status", "detail", "call")
	for _, e := range rows(doc, "entries") {
		fmt.Fprintf(out, toolFmt,
			trunc(str(e["created_at"]), 19), str(e["credential"]), str(e["upstream"]),
			str(e["method"]), str(e["tool"]), str(e["decision"]), str(e["status"]), str(e["detail"]),
			call(e))
	}
	return nil
}

// ApprovalAudit prints the approvals' own trail: filed, approved, denied.
func (c *Client) ApprovalAudit(out io.Writer, credential string) error {
	doc, err := c.Get("approval-audit", "/admin/approval-audit?credential="+url.QueryEscape(credential)+"&limit=50")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, approvalFmt, "created (UTC)", "credential", "kind", "subject",
		"action", "decided by", "bounds", "call")
	for _, e := range rows(doc, "entries") {
		fmt.Fprintf(out, approvalFmt,
			trunc(str(e["created_at"]), 19), str(e["credential"]), str(e["kind"]),
			str(e["subject"]), str(e["action"]), dash(e["decided_by"]), str(e["bounds"]),
			dash(e["arg_summary"]))
	}
	return nil
}

// binds says what a tool grant admits (P12): one CALL, named by the
// digest of its policy-relevant arguments. A tool grant with no digest is
// the closed legacy class — minted before argument binding, so it admits
// any arguments and says so; other kinds have no arguments at all.
func binds(g map[string]any) string {
	if str(g["kind"]) != "tool" {
		return "-"
	}
	if d := str(g["arg_digest"]); d != "" {
		return "call " + trunc(d, 12)
	}
	return "verb-level (legacy)"
}

// call renders the audited call: the human-readable summary built from the
// tool's declared policy fields, plus the digest prefix that ties a denial,
// its approval and the admitted call together.
func call(e map[string]any) string {
	summary, digest := str(e["arg_summary"]), str(e["arg_digest"])
	if digest == "" {
		return dash(summary)
	}
	if summary != "" {
		summary += " "
	}
	return summary + "[" + trunc(digest, 12) + "]"
}

// rows returns a document's list of records, tolerating a null or absent
// key exactly as the script's `d.get("entries") or []` does.
func rows(doc map[string]any, key string) []map[string]any {
	list, _ := doc[key].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// str renders a JSON value for a table cell. Numbers keep the digits the
// plane sent (the decoder is in UseNumber mode), and a null prints empty —
// the shell's behaviour, since these tables are read by eye and by grep.
func str(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case json.Number:
		return value.String()
	case bool:
		if value {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(value)
	}
}

// dash renders an absent optional as "-", which is what the columns for
// expiry, amount and the approver mean by "not set".
func dash(v any) string {
	if s := str(v); s != "" {
		return s
	}
	return "-"
}

func yesno(v any) string {
	if b, ok := v.(bool); ok && b {
		return "yes"
	}
	return "no"
}

// trunc cuts a cell to n characters, as the script's `e["created_at"][:19]`
// does — a timestamp is printed to the second, not to the microsecond.
func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
