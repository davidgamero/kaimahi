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
stack (`server/` in archived https://github.com/gambtho/tomte-old —
enforcement proxy, vault, spend metering, permit model, priced-pair gate)
before writing anything new.

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

   CRD reality at kagent 0.9.12 (verified against the live cluster,
   2026-08-31): no OpenRouter/GitHub Models provider exists — every
   OpenAI-compatible endpoint rides `provider: OpenAI` + `openAI.baseUrl`.
   kagent's `azureOpenAI` provider REQUIRES `apiVersion`, which conflicts
   with the v1 GA pin above — so the Azure path is also `provider: OpenAI`
   with the Foundry v1 base URL; do not use provider AzureOpenAI.
3. **P3 — connectors/tools** via MCP (kagent's native tool mechanism).
4. **P4 — governance** mounts at kagent's seams: ModelConfig BYO base_url →
   Tomte metering/enforcing proxy; kagent MCP tool server → Tomte enforcing
   gateway; permits/approvals compile down to kagent resources. Evaluate
   porting the archived old repo's `server/` first.

Target environments (D6): kind is the local/demo path; **AKS** is the named
managed-Kubernetes target. kagent runs on any conformant cluster — don't
build anything AKS-specific without a survey-backed justification.

## State of the world

| Lane | Owner | Status | Notes |
|------|-------|--------|-------|
| Repo bootstrap (LICENSE, README, CI, board) | coordinator | pushed to gambtho/tomte main | initial commit |
| P1: kagent hello world on kind | W1 worker | PR #2 MERGED (rebase e91ff88..a284923); coordinator verified (delta sheet below) | lane closed |
| README value-prop + Azure path (D6) | coordinator | PR #1 MERGED (verified on main, 94bbaef) | docs-only |
| P2: LLM-enhanced via ModelConfig | unassigned | GO — blindspot pass done, D7 recorded, W2 prompt ready (below) | contended: k8s/ + Makefile |
| P3–P4 | — | blocked on P2 merge | no pre-stacked PR bases |

## Decisions (user rulings, verbatim)

| # | Date | Decision | Verbatim quote |
|---|------|----------|----------------|
| D1 | 2026-08-31 | ~~Reuse gambtho/tomte; user overwrites it~~ SUPERSEDED by D5 | "we'll re-use the old one, after we have some content i will force push to overwrite the existing repo" |
| D2 | 2026-08-31 | ~~Do not archive the old repo~~ SUPERSEDED by D5 | "no, we can just overwrite it, the history may be useful" |
| D3 | 2026-08-31 | New repo is public | "Public" |
| D4 | 2026-08-31 | Coordinator may push board-doc-only commits direct to main | "Yes, board doc only (Recommended)" |
| D5 | 2026-08-31 | Old repo renamed to gambtho/tomte-old and archived; fresh gambtho/tomte created for the redux | "i changed my mind, i moved the existing tomte repo to gambtho/tomte-old and archived it. i'll create a new tomte repo for this" |
| D6 | 2026-08-31 | AKS is the named managed-Kubernetes target for the arc (kind stays the local/demo path); README gains a value-prop-over-kagent section and an Azure-path paragraph (GitHub Models phrasing per the P2 guardrail) | "i am wondering if we need to add more to our value proposition over kagent -- maybe also mention that we're ensuring smooth integration with AKS / github copilot models/ Azure AI foundry" — ruled via options: "Both (Recommended)", "Yes, record as D6 (Recommended)" |
| D7 | 2026-08-31 | P2 keyed live verification uses GitHub Models only; auth must flow through the GitHub CLI (`gh auth token` → K8s Secret, stdin-only). Other endpoints ship as documented presets marked not-live-verified | "github models, but we need to support login via github cli for it" |

Old-repo history is preserved at https://github.com/gambtho/tomte-old
(archived, read-only). No local checkout of it exists (deleted 2026-08-31);
clone from the archive when P4 port evaluation needs the source.

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

## Under consideration (not GO — do not build yet)

- **`npx tomte create agent`** — user feedback 2026-08-31: "one other good
  piece of feedback we should consider -- an npx tomte create agent command."
  Coordinator assessment: fits the leadership "simple cli" quote; fills the
  zero-to-cluster scaffolding gap kagent's runtime CLI doesn't own (P1's
  Makefile is this journey in glue form). Becomes a lane only after P1
  merges, and only with: (1) a written survey justifying it against kagent's
  CLI — scaffold/bootstrap only, no duplication of kagent runtime commands;
  (2) npm publishing deferred — `npx github:gambtho/tomte` suffices for dev;
  claiming the `tomte` npm name is an outward-facing naming commitment that
  needs explicit user approval (trademark counsel still owed on the name);
  (3) sequencing between P1 and P2 so P2 can extend the same scaffold.

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

### W2 — P2: LLM-enhanced via ModelConfig (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Tomte project (repo root: this checkout).
Read docs/COORDINATION.md first — prime directive, process rules, security
standing guidance, decisions D1–D7, and the P1 delta sheet all bind you.
Your lane: P2 — the hello-world stack from P1 upgraded so agents think with
hosted LLM endpoints via kagent ModelConfig.

Constraints:
- SURVEY FIRST (prime directive): record in the PR what kagent 0.9.12
  already ships for this and why each net-new file is justified. The board's
  P2 arc entry records CRD reality verified against the live cluster:
  OpenRouter / GitHub Models / Azure AI Foundry / any-compatible endpoints
  all use `provider: OpenAI` + `openAI.baseUrl`; do NOT use provider
  AzureOpenAI (its required apiVersion conflicts with the board's Foundry
  v1 GA pin — document this in the runbook).
- Deliverables:
  (a) Per-endpoint ModelConfig presets committed as YAML (suggested:
      k8s/models/): Anthropic, OpenAI, OpenRouter, GitHub Models, Azure AI
      Foundry (v1 GA), generic OpenAI-compatible base URL — plus the
      existing Ollama path. Every preset references keys ONLY via
      apiKeySecret/apiKeySecretKey. No key material or key-bearing field
      ever appears in YAML, ConfigMap, argv, env listings, or logs.
  (b) GitHub CLI login for GitHub Models (D7): a make target that checks
      `gh auth status`, then pipes `gh auth token` straight into
      `kubectl create secret ... --from-file=...=/dev/stdin` (stdin-only —
      never --from-literal, never a shell variable echoed anywhere).
      Document the scope caveat: the gh OAuth token is broader than needed;
      a fine-grained PAT with models:read is the least-privilege
      alternative. Phrasing guardrail: GitHub Models is "included with
      GitHub Copilot plans" — never claim api.githubcopilot.com support.
  (c) A way to switch the agent between presets (simplest mechanism that
      works; state your choice + alternatives in the PR).
  (d) Runbook section (extend docs/ from P1's pattern) including an
      explicit warning that P2 spend is ungoverned — metering arrives in
      P4.
  (e) CI stays KEYLESS — the repo is public and PR CI is fork-exposed; no
      repo secrets in workflows. Extend CI only with what runs keyless
      (e.g. preset YAML validated against the CRDs in the existing e2e
      cluster via kubectl apply --dry-run=server).
- Live verification (real, per process rules): GitHub Models end to end —
  gh-CLI-sourced Secret, preset applied, agent switched to it, `make chat`
  returns an A2A task state=completed with a non-empty reply. Paste
  evidence (commands + trimmed output) in the PR. P1 delta rule: a preset
  counts as live-verified only if actually invoked — schema-valid is not
  verified. Mark every other hosted preset "not live-verified" in the
  runbook. The keyless Ollama e2e must still pass at every commit.
- Branch from current main; PR targets main; no stacked bases. Lane ends at
  PR-open-with-checks-green — do not merge.
- Report deviations and surprises in the PR's "Deviations & decisions"
  section (delta sheet).
```

## Delta sheets from finished lanes

### P1 — kagent hello world on kind (PR #2, merged 2026-08-31)

Delivered on main: `k8s/hello-world.yaml` (ModelConfig + Agent — the
agent-as-code artifact), `k8s/ollama.yaml`, `k8s/kagent-values.yaml`,
`Makefile` (up/chat/down), `docs/P1-RUNBOOK.md`, CI `e2e-hello-world` job.

Coordinator verification (independent, 2026-08-31): tree confirmed on main
at a284923; live agent chatted via `make chat` (A2A task state=completed,
coherent self-description); live cluster diffed against origin/main — P1
payload identical, docs-only drift; pins confirmed (kagent 0.9.12,
qwen2.5:3b, keyless — zero Secret/key references in deliverables); main CI
run 33436458466 green including e2e.

Deviations (worker-reported, carried forward for P2+):

- **Model: qwen2.5:3b, not chart-default llama3.2** — kagent's python
  runtime (Google ADK) injects a builtin `ask_user` tool; small Llamas call
  it with malformed args and the invocation fails (`'str' object has no
  attribute 'get'`); system-message prohibition doesn't stop them. P2 model
  choices must be invocation-tested, not assumed.
- **kagent pinned v0.9.12** (0.10 is RC). `runtime: go` unusable at 0.9.12
  unless `controller.agentImage.registry=ghcr.io` is set (golang-adk image
  absent from default registry).
- **Chart sample agents/tool servers disabled** — one-agent demo; P3
  re-enables tooling deliberately.
- **kagent's bundled PostgreSQL runs in-cluster** — kagent brings its own
  store; Tomte added none (consistent with "cluster is the store" until P4,
  but note the cluster now contains a kagent-internal DB).
- **CI runners are 2-CPU** — Ollama resource requests were shrunk so kagent
  schedules; keep e2e resource budgets in mind for P2's larger flows.
