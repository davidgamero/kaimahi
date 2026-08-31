# Tomte coordination board

Single writer: the coordinator session. Worker sessions implement; they report
deviations and decisions here for ruling, and end their lane at
PR-open-with-checks-green. The user merges.

## Mission

Tomte makes agentic workflows accessible and safe to delegate.

Leadership goal, verbatim:

> "a template for an agent that creates a hello world agent running on a k8s
> cluster, then expand to leverage llm to enhance the agent, allow connectors,
> etc — use a simple cli to get the agent running on k8s"

> "having an artifact that shows my agent topology — almost agent as code
> (ideally yaml template or something like that)"

CLI before UI. Simplest possible solution.

## Prime directive

**DO NOT REBUILD WHAT EXISTS.** This caused the restart.

kagent (kagent.dev) already ships (verified 2026-08-31): declarative K8s
agents (Agent CRD YAML — which IS the agent-as-code topology artifact, A2A
agent cards included), a CLI + dashboard, a broad model-provider list, and MCP
tool integration. **kagent is the agent runner.** Tomte's product is the
governance plane kagent verifiably lacks: budgets and spend metering, approval
workflows and blast-radius permits, credential custody (keys never reach the
agent), egress enforcement, and audit.

Before ANY component is built, the owning session must survey what exists and
justify net-new **in writing** (in its PR). Same directive both directions:
when governance mounts, evaluate porting the old repo's verified working Go
stack (`../tomte-old/server/` — enforcement proxy, vault, spend metering,
permit model, priced-pair gate) before writing anything new.

## The arc

1. **P1 — hello world**: kagent on a kind cluster; hello-world agent as a
   kagent Agent YAML; driven end to end via CLI. This is the leadership demo;
   the YAML is the artifact.
2. **P2 — LLM-enhanced**: via kagent ModelConfig. Endpoint targets that matter
   to leadership: Anthropic, OpenAI, OpenRouter, GitHub Models (say "included
   with GitHub Copilot plans"; never claim api.githubcopilot.com support —
   undocumented, token-exchange only), Azure AI Foundry (pin the v1 GA API —
   plain OpenAI-compatible, no api-version param), any OpenAI-compatible base
   URL, local models.
3. **P3 — connectors/tools** via MCP (kagent's native tool mechanism).
4. **P4 — governance** mounts at kagent's seams: ModelConfig BYO base_url →
   Tomte metering/enforcing proxy; kagent MCP tool server → Tomte enforcing
   gateway; permits/approvals compile down to kagent resources. Evaluate
   porting `../tomte-old/server/` first.

## State of the world

| Lane | Owner | Status | Notes |
|------|-------|--------|-------|
| Repo bootstrap (LICENSE, README, CI, board) | coordinator | pushed to gambtho/tomte main | initial commit |
| P1: kagent hello world on kind | unassigned | prompt ready (below); GO | contended dir: whole repo until it lands |
| P2–P4 | — | blocked on P1 merge | no pre-stacked PR bases |

## Decisions (user rulings, verbatim)

| # | Date | Decision | Verbatim quote |
|---|------|----------|----------------|
| D1 | 2026-08-31 | ~~Reuse gambtho/tomte; user overwrites it~~ SUPERSEDED by D5 | "we'll re-use the old one, after we have some content i will force push to overwrite the existing repo" |
| D2 | 2026-08-31 | ~~Do not archive the old repo~~ SUPERSEDED by D5 | "no, we can just overwrite it, the history may be useful" |
| D3 | 2026-08-31 | New repo is public | "Public" |
| D4 | 2026-08-31 | Coordinator may push board-doc-only commits direct to main | "Yes, board doc only (Recommended)" |
| D5 | 2026-08-31 | Old repo renamed to gambtho/tomte-old and archived; fresh gambtho/tomte created for the redux | "i changed my mind, i moved the existing tomte repo to gambtho/tomte-old and archived it. i'll create a new tomte repo for this" |

Old-repo history is preserved at https://github.com/gambtho/tomte-old
(archived, read-only) and locally at `../tomte-old`. (The redux checkout now
lives at `../tomte`; the old local checkout's `origin` URL still says
`gambtho/tomte` from before the rename — trust its contents, not its remote.)

## Considered and rejected (do not relitigate)

- **Building our own agent runtime / CRDs / dashboard** — rejected; kagent
  ships them. This mistake caused the restart.
- **Building a Tomte CLI for P1 by default** — kagent has a CLI. Net-new CLI
  code requires a written survey-based justification in the PR. A thin
  Makefile/script wrapper over kagent+kind is acceptable glue.
- **A database for the K8s track** — rejected; the cluster is the store
  (Secrets, ConfigMaps, resource status). A durable store arrives only with
  the governance plane (P4).
- **Overwriting the old repo in place via force-push** — considered (D1/D2),
  reversed by D5: old repo lives on as archived gambtho/tomte-old; redux gets
  a fresh gambtho/tomte.
- **Blanket $0 pricing inferred from URL/provider** — rejected; local/free is
  an explicit user-answered classification (GitHub Models has opt-in paid
  billing).

## Process rules (proven over ~60 PRs; keep)

- Board is the single coordination doc; coordinator is the only writer.
- One session owns a contended directory at a time; docs and independent dirs
  parallelize freely.
- Every PR targets main; NO pre-stacked PR bases — each phase waits for its
  predecessor to merge. A GitHub MERGED status is not proof work is on main:
  verify against the tree.
- Check a PR's state before every push to its branch; if it merged, branch
  fresh.
- Worker lanes end at PR-open-checks-green; the user merges.
- Verification is real: run the command, boot the thing, hit the cluster.
  Suite green at every commit. Coordinator verifies reported results
  independently before recording (verify parameters, not just mechanisms).
- Outward-facing actions (other people's repos, publishing) need the user's
  approval naming the exact artifact.
- Ask the user the few load-bearing shaping questions BEFORE a big build;
  leadership quotes go on the board verbatim.

## Security standing guidance (already paid for)

- Fail closed everywhere: a verify path accepts only a well-formed positive
  (WAFs return HTML 2xx; OpenRouter-class gateways return 200 with an error
  envelope for bad keys).
- Keys: stdin-only capture, stored in K8s Secrets, never in
  YAML/ConfigMap/argv/env-listings/logs. Go's HTTP client strips Authorization
  on cross-host redirects but NOT custom headers like x-api-key — refuse
  redirects on keyed calls.
- No blanket $0 pricing by inference (see rejected list).
- Record spend before honoring failures: every billed provider call gets
  ledgered even when the surrounding operation fails.
- K8s track needs no database — the cluster is the store until P4.

## Ready-to-paste worker prompts

### W1 — P1: kagent hello world on kind (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Tomte project (repo root: this checkout).
Read docs/COORDINATION.md first and follow its process rules and prime
directive exactly. Your lane: P1 — kagent hello world on a kind cluster,
driven end to end via CLI, with the kagent Agent YAML as the deliverable.

Constraints:
- SURVEY FIRST. Before writing anything net-new, survey what kagent already
  ships (kagent CLI, helm charts, Agent/ModelConfig CRDs, quickstart docs) and
  record in your PR description what exists and why each net-new file is
  justified. Do NOT build a Tomte CLI, controller, or dashboard. A thin
  Makefile or script wrapper over kind + kagent CLI is acceptable glue.
- Deliverables: (a) the hello-world Agent YAML committed to the repo (this is
  the leadership demo artifact); (b) a runbook (docs/ or README section) with
  the exact commands from empty machine to talking to the agent; (c) whatever
  minimal glue the survey justifies; (d) CI extended only if you add something
  CI can actually check.
- kagent agents need a ModelConfig. Prefer the cheapest/simplest working
  option and state your choice + alternatives in the PR. If any API key is
  involved: stdin-only capture, K8s Secret only — never in YAML, ConfigMap,
  argv, env listings, or logs.
- Verification is real: actually create the kind cluster, install kagent,
  apply the YAML, and converse with the agent via CLI. Paste the evidence
  (commands + trimmed output) into the PR description. Suite green at every
  commit.
- Branch from current main; PR targets main; no stacked bases. Your lane ends
  at PR-open-with-checks-green — do not merge.
- Report to the coordinator (via the PR description's "Deviations & decisions"
  section) anything you decided that the board doesn't already rule on, and
  anything that surprised you (delta sheet).
```

## Delta sheets from finished lanes

(none yet)
