# Kaimahi coordination board

(Project renamed tomte → kaimahi, D9/D10; historical quotes and delta
sheets below keep the old name verbatim.)

Single writer: the coordinator session. Worker sessions implement; they report
deviations and decisions here for ruling, and end their lane at
PR-open-with-checks-green. The user merges.

## Mission

Kaimahi makes agentic workflows accessible and safe to delegate.

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
tool integration. **kagent is the agent runner.** Kaimahi's product is the
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
   to leadership: Anthropic, OpenAI, OpenRouter, GitHub Copilot subscription
   (per D8: api.githubcopilot.com directly; the pre-D8 "never claim
   api.githubcopilot.com support" guardrail is superseded, but its caveat —
   undocumented API surface, expiring token — must stay documented; GitHub
   Models itself RETIRED 2026-07-30, verified 410), Azure AI Foundry (pin
   the v1 GA API — plain OpenAI-compatible, no api-version param), any
   OpenAI-compatible base URL, local models. DELIVERED by PR #3.

   CRD reality at kagent 0.9.12 (verified against the live cluster,
   2026-08-31): no OpenRouter/GitHub Models provider exists — every
   OpenAI-compatible endpoint rides `provider: OpenAI` + `openAI.baseUrl`.
   kagent's `azureOpenAI` provider REQUIRES `apiVersion`, which conflicts
   with the v1 GA pin above — so the Azure path is also `provider: OpenAI`
   with the Foundry v1 base URL; do not use provider AzureOpenAI.
3. **P3 — connectors/tools** via MCP (kagent's native tool mechanism).
4. **P4 — governance** mounts at kagent's seams: ModelConfig BYO base_url →
   Kaimahi metering/enforcing proxy; kagent MCP tool server → Kaimahi enforcing
   gateway; permits/approvals compile down to kagent resources. Evaluate
   porting the archived old repo's `server/` first.

5. **P5 — the undeniable demo** (D14). The P1–P4 arc is COMPLETE and
   CI-asserted, but it governs an agent that lists ConfigMaps — nothing
   in the demo needs governance. P5 is not new capability; it makes the
   built capability legible and credible: **P5a** a governed Slack
   outbound path where posting requires a P4c approval (the first
   consequential action in the repo), **P5b** cluster portability plus a
   real AKS deployment (the README has claimed AKS since D6; the
   Makefile's `KUBE_CTX := kind-$(KIND_CLUSTER)` means it cannot even
   target one). Demos run on Copilot; CI stays keyless on ollama.

Target environments (D6): kind is the local/demo path; **AKS** is the named
managed-Kubernetes target. kagent runs on any conformant cluster — don't
build anything AKS-specific without a survey-backed justification. Note
(2026-09-01): AKS has never been exercised — P5b closes that gap. Known
kind-specific obstacles for that lane: `imagePullPolicy: Never` plus
`kind load docker-image` (deliberate for kind, unusable on AKS — needs a
registry story), the Postgres PVC's storage class, and the `kind-` context
prefix.

## State of the world

| Lane | Owner | Status | Notes |
|------|-------|--------|-------|
| Repo bootstrap (LICENSE, README, CI, board) | coordinator | pushed to gambtho/tomte main | initial commit |
| P1: kagent hello world on kind | W1 worker | PR #2 MERGED (rebase e91ff88..a284923); coordinator verified (delta sheet below) | lane closed |
| README value-prop + Azure path (D6) | coordinator | PR #1 MERGED (verified on main, 94bbaef) | docs-only |
| P2: LLM-enhanced via ModelConfig | W2 worker | PR #3 MERGED (d1a584d, tree-identical to checks-green branch); coordinator verified (delta sheet below) | lane closed |
| P3: connectors/tools via MCP | W3 worker | PR #4 MERGED (99edd8a); coordinator verified incl. live tool call (delta sheet below) | lane closed |
| Rename lane: in-repo tomte → kaimahi (D9/D10) | rename worker | PR #5 MERGED (01f5c3c); coordinator verified (delta sheet below); board renamed by coordinator | lane closed |
| P4a: metering/enforcing LLM proxy (D11) | W4 worker | PR #12 MERGED; coordinator verified live incl. budget denial + custody (delta sheet below) | lane closed |
| P4b: enforcing MCP gateway | W5 worker | PR #15 MERGED (97c2b5f, payload identical to verified 06873d2; post-merge main CI green); delta sheet below | lane closed |
| P4c: approvals/permits (D13) | W7 worker | PR #17 MERGED (dd08f00); coordinator verified both approval cycles independently pre-merge (delta sheet below) | lane closed — ARC COMPLETE |
| P5a: governed Slack connector (D14) | W8 worker | PR #18 MERGED; coordinator verified (custody, and the discovery finding reproduced independently); delta sheet below | lane closed |
| P5b: cluster portability + real AKS run (D14/D15) | W9 worker | PR #19 MERGED; coordinator verified (leak scan, teardown, guard, kind regression) — delta sheet below | lane closed |
| P7a: NetworkPolicy egress | W10 worker | PR #23 MERGED (7fd0e3f); coordinator verified — negative matrix reproduced on the lane's cluster (delta sheet below) | lane closed; doc reconciliation owed (see sheet) | PARALLEL SET (see rules below); own cluster `netpol-verify` |
| P7b: P6 inbound connectors | W11 worker | PR #24 MERGED; coordinator verified via CI matrix + code read (delta sheet below) | lane closed | PARALLEL SET; own cluster `inbound-verify`; the big one |
| P7c: docs restructure (capability, not chronology) | W12 worker | PRs #21/#22 MERGED (8a3e568, 29e031c); coordinator verified (delta sheet below) | lane closed | PARALLEL SET; owns `docs/` structure; no cluster needed |
| Post-move: Go module path + owner refs (D16) | W13 worker | PR #26 MERGED; verified (delta sheet below) | lane closed |
| CI hygiene: verifier reads function_response; docs-only e2e short-circuit | W14 worker | PR #29 MERGED; verified (delta sheet below) — this board PR is the first live docs-only test of the short-circuit | lane closed |
| AKS NetworkPolicy enforcement (P7a finding) | W15 worker | PR #30 MERGED; verified incl. teardown (delta sheet below) | lane closed |
| Post-P7a/P7b reconciliation | coordinator | PR #28 MERGED | lane closed |
| CLI decisions + PR #16 review | user + coordinator | awaiting the user's five CLI-PROPOSAL rulings | not a build lane; parallelises with everything |
| CI flake: agent-readiness race (P5b finding) | coordinator — PR #20 MERGED (73917e9) after a review round: retry anchored to the controller's whole error line; slack-post retries only unambiguous failures | User ruling 2026-09-01: fold into the next phase rather than a standalone micro-lane — as its **FIRST commit, before feature work**, so the lane's own CI is not reddened by someone else's race | retry predicate covers `connection refused` but not `EOF`; main went red once then green on re-run. Widen narrowly (EOF, connection-reset) so it cannot mask a real outage — see P5b delta sheet |
| NetworkPolicy egress (promoted 2026-09-01) | — | candidate, not GO | P5a put a deliberate internet-egress pod in the cluster; three non-network layers bound blast radius today. Strongest-argument-yet per P5a's own accounting |
| P6: inbound connectors (webhooks/user APIs) | — | parked candidate; own blindspot pass when reached | genuine net-new surface: ingress auth, replay, rate limits, every event causes spend |
| CLI: `kaimahi agent create` (Tatsinnit, PR #16) | teammate | D19 ruled; coordinator review in progress (security posture, scope, tests, CI) | not published; internal via npx github: |
| Docs: CLI-first framing + naming record | teammate (Tatsinnit) | PR #10 MERGED (ratifies D12) | staleness fixes folded into reconciliation lane |
| Docs: agent-first scenarios | teammate (Tatsinnit) | PR #11 MERGED (authors' public credit ratified by user merge) | lane closed |
| Post-merge reconciliation | coordinator | PR #13 MERGED (0ce72ca, main CI green incl. hardened secret scan) | lane closed |
| User docs (guide + FAQ, shipped functionality only) | W6 worker | PR #14 MERGED (verified on main, 65c551d); coordinator-reviewed (fact-check + voice grep clean) | lane closed; shared-cluster collision recorded in its deviations |

## Decisions (user rulings, verbatim)

| # | Date | Decision | Verbatim quote |
|---|------|----------|----------------|
| D1 | 2026-08-31 | ~~Reuse gambtho/tomte; user overwrites it~~ SUPERSEDED by D5 | "we'll re-use the old one, after we have some content i will force push to overwrite the existing repo" |
| D2 | 2026-08-31 | ~~Do not archive the old repo~~ SUPERSEDED by D5 | "no, we can just overwrite it, the history may be useful" |
| D3 | 2026-08-31 | New repo is public | "Public" |
| D4 | 2026-08-31 | ~~Coordinator may push board-doc-only commits direct to main~~ SUPERSEDED by D17 | "Yes, board doc only (Recommended)" |
| D5 | 2026-08-31 | Old repo renamed to gambtho/tomte-old and archived; fresh gambtho/tomte created for the redux | "i changed my mind, i moved the existing tomte repo to gambtho/tomte-old and archived it. i'll create a new tomte repo for this" |
| D6 | 2026-08-31 | AKS is the named managed-Kubernetes target for the arc (kind stays the local/demo path); README gains a value-prop-over-kagent section and an Azure-path paragraph (GitHub Models phrasing per the P2 guardrail) | "i am wondering if we need to add more to our value proposition over kagent -- maybe also mention that we're ensuring smooth integration with AKS / github copilot models/ Azure AI foundry" — ruled via options: "Both (Recommended)", "Yes, record as D6 (Recommended)" |
| D7 | 2026-08-31 | ~~P2 keyed live verification uses GitHub Models only; auth must flow through the GitHub CLI (`gh auth token` → K8s Secret, stdin-only)~~ SUPERSEDED by D8: GitHub Models is retired and gh tokens are not Copilot-entitled | "github models, but we need to support login via github cli for it" |
| D8 | 2026-08-31 | P2's keyed path is the Copilot subscription's model API directly (api.githubcopilot.com, no local proxy), superseding D7. Forced by two verified facts: GitHub Models retired 2026-07-30 (endpoint returns 410) and gh CLI tokens fail the Copilot token exchange (403) — device flow required. The endpoint's undocumented-surface caveat must stay documented wherever the preset appears | ruled mid-lane in the P2 worker session (not captured verbatim); recorded per PR #3 "Deviations & decisions" item 2 and the user-relayed close-out; ratified by the user's merge of PR #3 |
| D9 | 2026-08-31 | TENTATIVE rename: tomte → **kaimahi** (te reo Māori: worker). No changes yet — no repo/README/board/package renames until the user says go. Still owed before final: the NZ developer's read + Māori cultural appropriateness, and trademark counsel. Availability as checked 2026-08-31 (decays — nothing claimed): npm kaimahi + create-kaimahi, PyPI, crates, kaimahi.dev/.io all free; claiming any of them is outward-facing and needs explicit user approval naming the artifact | "lets tentatively go with a rename to kaimahi, but lets not make the changes yet" |
| D10 | 2026-08-31 | Repo rename executed ahead of D9's freeze: user renamed the GitHub repo (initially to "kaiwahi" — a typo; coordinator caught the m/w mismatch vs D9 and, with user approval, corrected it to **gambtho/kaimahi**). The in-repo rename (README, board, Makefile names, docs) is a lane queued to run AFTER P3 merges. D9's remaining gates (cultural read, counsel) still stand for the name going truly final | "i changed the repo name to kaiwahi -- whenever p3 finishes we should do the rename change" — then ruled via option: "kaimahi — fix repo (Recommended)" |
| D11 | 2026-08-31 | P4 shaping: (1) the metering/enforcing LLM proxy leads (P4a); MCP gateway (P4b) and approvals (P4c) follow as separate lanes. (2) The durable store is in-cluster Postgres. (3) The P4 demo is CLI-only | ruled via options: "LLM proxy first (Recommended)", "In-cluster Postgres (Recommended)", "Yes, CLI only (Recommended)" |
| D12 | 2026-09-01 | README positioning: CLI-first/incubation framing leads; the governance plane is presented as the incubated thesis. Supersedes D6's framing (D6's substance — the five governance controls and the AKS/Foundry paragraph — is retained). The agent-first scenario doc with four named authors is published under MIT. Both ratified by the user merging PRs #10/#11 after coordinator review | "sure, go ahead" (post the reviews) → "ok, that merged as well" — ratified by merge |
| D15 | 2026-09-01 | P5b shaping: (1) the plane image goes to a **private ACR** (`az acr build` + AKS attach) — deliberately NOT a public ghcr image, which would be an outward-facing artifact and a soft public claim on the provisional name while D9's gates are open; (2) the **worker creates and tears down** the AKS cluster with the already-authenticated `az` CLI (same pattern as `gh`), with teardown MANDATORY at lane end and a reported spend estimate; (3) the AKS path is **Copilot-only — no Ollama** (the keyless path is already CI-proven on kind every PR; AKS's job is proving the plane runs on a managed cluster with a real model) | ruled via options: "ACR, private (Recommended)", "Worker creates and tears down (Recommended)", "Copilot-only on AKS (Recommended)" |
| D16 | 2026-09-01 | Repo moved to a GitHub ORGANIZATION: **kaimahi-agents/kaimahi** (public). Old paths redirect (gambtho/kaimahi, gambtho/tomte). All `gh -R` targets, worker prompts and docs should name the org from now on. Note for D9: creating an org named for the project is a stronger public claim on the still-provisional name than the repo rename was; D9's gates (cultural read, trademark counsel) remain open and are now more urgent, and NAMING.md's "nothing here is claimed" is no longer strictly true | "i moved the repo to an oranization - https://github.com/kaimahi-agents/kaimahi" |
| D17 | 2026-09-01 | Board updates go through PULL REQUESTS from now on — supersedes D4. Context: the org move brought the `protect-main` ruleset (PR required + hygiene/go-plane/e2e as required checks; admins may bypass), and the coordinator's direct board pushes were landing via that bypass. The coordinator remains the board's single WRITER; the change is that every board edit is a PR the user merges. Practical consequence: each board PR waits on the full e2e (~11 min) — a docs-only short-circuit for the e2e job is a small CI follow-up so a doc-only PR still reports all three required checks without booting a cluster. This row is itself the first board PR | "i think we should start doing PRs for board updates." |
| D18 | 2026-09-01 | The Slack app's `chat:write.public` scope (bot may post to any public channel uninvited — flagged by P5a, recommended for removal) is ACCEPTED as-is; item closed | "i'm not worried about the slack permissions" |
| D19 | 2026-09-01 | CLI rulings (the five open decisions in docs/CLI-PROPOSAL.md; sequencing is moot now P4 shipped): (1) **do NOT publish to npm yet** — internal use via `npx github:kaimahi-agents/kaimahi`; publishing is a one-line decision once D9's gates clear; (2) **scaffold-only** — `agent create` is the only command, R/U/D refused by printing the kubectl/kagent command that already does the job; (3) **the Makefile owns cluster bring-up** — no `kaimahi up`/`install`; (4) **a zero-runtime-dependency Node toolchain is accepted** into the repo (`cli/`, with `make cli-test` in CI). PR #16 moves from parked prototype to coordinator review against these | ruled via options: "Not yet — internal via npx github: (Recommended)", "Scaffold-only, as built (Recommended)", "Makefile owns bring-up (Recommended)", "Yes, zero-dependency Node (Recommended)" |
| D14 | 2026-09-01 | P5 direction: the **undeniable demo** — not a new capability arc but making the built one legible and credible. Rulings: (1) outbound connector platform is **Slack** (via existing MCP servers, no connector code); (2) AKS work goes all the way — cluster portability AND a real AKS deployment with evidence (accepts Azure spend + credentials in a worker session); (3) demos run on the **Copilot** preset while **CI stays keyless on ollama** (public fork-exposed repo — no repo secrets in CI, ever). Rationale on the board: everything governed so far protects an agent that lists ConfigMaps; posting to a channel humans read is the first consequential action, and it makes the approval gate the point rather than the plumbing | "sure, that's undeniable demo makes sense" — then ruled via options: "Slack (Recommended)", "Portability + real AKS run (Recommended)", "Copilot for demo, ollama for CI (Recommended)" |
| D13 | 2026-09-01 | P4c approval model: TIME-BOXED PERMITS — a denied action files a pending request; approval grants it bounded (expiry by duration and/or use count) and compiles into the existing allowlist/budget rows; deny-and-retry mechanics, no held-open calls. Demo scenarios: tool-access widening (k8s_get_events, read-only) AND budget overage; the P3 tool-server read-only posture stays untouched (write-tool demo deferred) | ruled via options: "Time-boxed permits (Recommended)"; "Widen tool access (Recommended), Budget overage (Recommended)" |

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

- **`make up` guard for governed agents** (W6 finding, 2026-09-01):
  `make up` re-applies `k8s/hello-world.yaml`, silently re-pointing the
  agent at the ungoverned model — governance quietly drops off after any
  re-run (FAQ-documented). A make-level guard (detect a governed
  modelConfig and warn or re-govern) is a small, well-scoped fix — fold
  into the P4b lane's close-out or a follow-up micro-lane.

- ~~Connectors outbound~~ → **GO as P5a** (D14, Slack). Inbound remains
  parked below as P6.
- ~~`npx kaimahi create agent`~~ → **ruled by D19**: scaffold-only, Node
  zero-dep, Makefile owns bring-up, NOT published yet. PR #16 under review.
- **Connectors: outbound (Slack/Discord) + inbound (user APIs, webhooks,
  common sources)** — user feedback 2026-09-01: "i think a piece of
  functionality we should consider adding is the creation of connectors --
  output to discord/slack -- but also inbound from user provided api, or
  other common sources." Coordinator assessment: OUTBOUND is configuration,
  not construction — Slack/Discord MCP servers exist in the ecosystem and
  kagent's MCPServer/RemoteMCPServer deploys them; the real work is
  governance (tokens in plane custody, calls through the P4b gateway
  allowlist + audit, channel-posting as the natural P4c approvals demo).
  Prime directive: no connector code without a survey showing the gap.
  INBOUND is the genuine net-new surface: an event→A2A bridge (webhook →
  agent invoke) IF the survey finds nothing upstream; must reuse the
  plane's kmh_ credential model for inbound auth and sit behind P4a
  budgets (inbound events cause spend), with ingress security (auth,
  replay, rate limits) as first-class requirements. Sequencing: outbound
  folds into the P4c demo; inbound is a P5 lane after P4 completes, with
  its own blindspot pass and shaping questions. Cross-links: SCENARIOS.md
  billing journey argues for exactly this; CLI-PROPOSAL --tools flag
  would scaffold the outbound wiring.

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
  Since D17 every board change is a PR the user merges (the `protect-main`
  ruleset requires PR + three checks; no more direct pushes).
- One session owns a contended directory at a time; docs and independent dirs
  parallelize freely.
- A live cluster is a contended resource like a directory: the shared
  kaimahi-p1 belongs to the open lane that deploys to it; every other
  session (coordinator verification included) uses its own
  `KIND_CLUSTER=<name>` cluster while that lane is open. (Learned
  2026-09-01: a parallel docs lane's `make plane` from main reverted the
  P4b worker's in-progress gateway deployment.)
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
- Key-bearing shell steps live in standalone scripts with
  `set -euo pipefail`, never in make recipes: make runs recipes under dash
  with no pipefail, and a failed pipe stage can fail OPEN (P2 caught a
  make-recipe draft storing an empty Secret on a failed token exchange).
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

### W3 — P3: connectors/tools via MCP (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Tomte project (repo root: this checkout).
Read docs/COORDINATION.md first — prime directive, process rules, security
standing guidance, decisions D1–D9, and BOTH delta sheets (P1, P2) bind
you. Your lane: P3 — the demo agent gains connectors/tools via MCP,
kagent's native tool mechanism.

Constraints:
- SURVEY FIRST (prime directive): kagent 0.9.12 ships the whole MCP stack
  (verified on the live cluster): an MCPServer CRD (v1alpha1) that deploys
  a tool server in-cluster (stdio transport via a sidecar gateway spawning
  uvx/npx per session — 2-8s startup, mind timeouts — or http), a
  RemoteMCPServer CRD (v1alpha2, SSE/STREAMABLE_HTTP) for existing
  endpoints, and Agent.spec.declarative.tools[] wiring (type: McpServer,
  headersFrom, allowedHeaders). Your survey must also settle the
  ToolServer-vs-MCPServer/RemoteMCPServer version split — which is the
  supported path at 0.9.12 — and record it. Tomte builds NO MCP runtime,
  proxy, or gateway machinery — the enforcing MCP gateway is P4. Net-new
  is CRD data, thin Makefile/script glue, docs, and CI only; justify each
  file in the PR.
- Deliverables:
  (a) A tool server as committed YAML (k8s/ pattern): prefer the simplest
      useful MCP server, keyless, deterministic, and no external egress if
      achievable (CI must be able to assert its output fail-closed). State
      your choice + alternatives in the PR.
  (b) The agent wired to it via spec.declarative.tools. Precedent from P2:
      k8s/hello-world.yaml (the P1 artifact) is never mutated — extend via
      a patch mechanism like make use, or a separate tools-enabled Agent
      YAML; choose the simplest and state alternatives.
  (c) Live verification MUST prove a real tool call happened — not just a
      Ready agent or a plausible answer. Ask something only the tool can
      answer and evidence the invocation (tool-server logs, kagent
      events/usage). P1 delta rule applies with force: qwen2.5:3b must be
      invocation-tested calling YOUR tool; if it misfires, test candidate
      models (make model MODEL=...) and document the working pin. CI stays
      keyless and within the 2-CPU runner budget (P2 delta). The Copilot
      preset may serve extra local evidence but never CI.
  (d) docs/P3-RUNBOOK.md following the P1/P2 pattern, including an
      explicit warning that P3 tools are ungoverned — egress enforcement
      and tool permits arrive in P4.
  (e) CI: extend the keyless e2e with the tool path, fail-closed (reuse
      scripts/verify-chat.py where it fits); existing P1/P2 e2e steps stay
      green at every commit.
- Security guidance binds: no secrets in YAML/argv/env/logs anywhere; the
  demo tool should need no auth at all — if auth is unavoidable, use
  headersFrom + a Secret captured stdin-only via a pipefail script (never
  a make recipe).
- Branch from current main; PR targets main; no stacked bases. Lane ends
  at PR-open-with-checks-green — do not merge.
- Report deviations and surprises in the PR's "Deviations & decisions"
  section (delta sheet).
```

### W-RENAME — in-repo rename tomte → kaimahi (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for this project (repo root: this checkout — now
gambtho/kaimahi on GitHub; old gambtho/tomte URLs redirect). Read
docs/COORDINATION.md first — process rules and decisions D9/D10 govern
this lane. Your lane: the in-repo rename tomte → kaimahi.

Scope (rename in): README.md (title, prose, and the working-name footnote —
keep the no-trademark-claimed wording, now for "kaimahi", and state
factually that kaimahi is te reo Māori for "worker"; nothing more —
cultural acknowledgment wording beyond that fact awaits D9's pending
cultural read), docs/P1/P2/P3 runbooks, Makefile, scripts/, k8s/ (comments
AND agent systemMessages — mutating k8s/hello-world.yaml is explicitly
authorized for this lane only; the P1-artifact never-mutate precedent
yields to an identity change), .github/workflows/.

Specific decisions, choose and state in the PR:
- KIND_CLUSTER tomte-p1 → kaimahi-p1 (or argue otherwise). Document the
  local-migration note: existing tomte-p1 clusters keep working via
  KIND_CLUSTER=tomte-p1, or `kind delete cluster --name tomte-p1` and a
  fresh `make up`.
- scripts/copilot-secret.sh: TOMTE_COPILOT_TOKEN_FILE env var and
  ~/.config/tomte/ path → kaimahi equivalents; decide whether to honor the
  old location once (simple mv note in the runbook is acceptable).

Explicitly OUT of scope:
- docs/COORDINATION.md — coordinator-owned; do not touch it.
- Anything outward-facing: no npm/PyPI/crates/domain/org claims, no
  GitHub settings changes (the repo rename is already done). D9's gates
  (cultural read, trademark counsel) are not yours to close.
- Links to https://github.com/gambtho/tomte-old — historical, keep as-is.

Verification: after the rename run a full audit — `grep -riIn tomte .`
(excluding .git and docs/COORDINATION.md) — and list every surviving hit
in the PR with its justification (tomte-old links should be the bulk).
Repo-URL references should point at gambtho/kaimahi, not rely on
redirects. Full CI must stay green (the e2e exercises P1+P2+P3 paths);
run `make up`/`make chat` locally if you change anything load-bearing in
the Makefile.

Branch from current main; PR targets main; no stacked bases. Lane ends at
PR-open-with-checks-green — do not merge. Report deviations in the PR's
"Deviations & decisions" section.
```

### W4 — P4a: metering/enforcing LLM proxy (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D11, and ALL delta sheets
bind you. Your lane: P4a — the first governance slice: a metering and
enforcing LLM proxy, mounted at kagent's ModelConfig baseUrl seam (D11).

PORT EVALUATION FIRST (prime directive, both directions): clone the
archived https://github.com/gambtho/tomte-old and evaluate porting its
verified Go stack before writing anything new. Coordinator's inventory to
seed your survey: server/ is ~9k LOC across 22 packages, but it is the
OLD architecture's full control plane — its engine/scheduler/reaper,
harness, httpapi/session shell, and workflow model are REPLACED by kagent;
do not port them. The governance core is what you evaluate:
internal/{proxy,proxyadapter,meter,permit,vault,llm,redact} and the
store/db layer they drag in (Postgres 16 — sanctioned by D11). Record in
the PR, per package: port / adapt / rewrite / skip, with reasons.

Architecture (board + D11):
- The plane runs in-cluster: namespace kaimahi, proxy Deployment/Service,
  Postgres 16 Deployment as the durable store, a migrations step.
- Mount: a governed ModelConfig preset whose openAI.baseUrl points at the
  proxy; the proxy forwards upstream. Upstreams in scope: exactly the two
  live-verified paths — in-cluster ollama (free tier of the demo) and the
  Copilot subscription endpoint (D8 semantics: expiring token, custody
  rules). No other upstreams in this lane.
- Credential custody: real upstream credentials live only with the proxy
  (Secret mounted to it); the agent's governed preset carries a
  Kaimahi-issued opaque credential, never the real key. Keys never reach
  the agent — this is the mission sentence, prove it in evidence.
- Budgets fail CLOSED: an exhausted budget denies with a clear error.
  Ledger records spend BEFORE honoring failures (standing guidance).
  Pricing: no blanket $0 by inference — ollama is $0 only as an explicit
  classification; Copilot usage is counted (tokens) and priced only if a
  real price is configured (the old repo's priced-pair gate is the
  pattern). Never invent prices.
- Security guidance binds throughout: fail-closed verify paths, stdin-only
  key capture via pipefail scripts, no redirects on keyed calls, redaction
  in logs (port redact), no key material in YAML/argv/env/logs.

Deliverables:
(a) The Go code in a top-level module dir (choose the name, state why),
    `go test ./...` green at every commit.
(b) k8s manifests for the plane + the governed ModelConfig preset.
(c) Makefile glue: deploy the plane, set a budget, chat through the
    governed preset, show the ledger, demonstrate budget exhaustion
    failing closed — CLI only (D11), following the make-target style of
    P1–P3.
(d) docs/P4A-RUNBOOK.md per the runbook pattern, including what is now
    governed vs still ungoverned (MCP/tools until P4b; approvals until
    P4c).
(e) CI: Go build+test job; keyless e2e extension driving the governed
    ollama path (chat via proxy → ledger row asserted → budget denial
    asserted, fail-closed). Mind the 2-CPU budget (P3 delta: node was
    ~95% requests before shrinking) — a separate job or trimmed resources
    may be needed; state your choice.

Out of scope: MCP gateway (P4b), approval workflows beyond what budgets
need (P4c), any UI, new model endpoints, npm/domain/external claims.

Verification is real: live cluster evidence in the PR — a governed chat
that works, the ledger rows for it, the same chat denied after budget
exhaustion, and proof the real key never appears agent-side (e.g. the
governed preset's Secret contents vs the proxy's). Suite green at every
commit. Branch from current main; PR targets main; no stacked bases. Lane
ends at PR-open-with-checks-green — do not merge. Report deviations in
the PR's "Deviations & decisions" section.
```

### W5 — P4b: enforcing MCP gateway (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D12, and ALL delta sheets
bind you (P4a's especially). Your lane: P4b — the governance plane's
second slice: an enforcing MCP gateway at kagent's tool-server seam.

DESIGN SOURCES FIRST (prime directive): (a) kagent 0.9.12's shipped MCP
stack is on the board's P3 entries — RemoteMCPServer (STREAMABLE_HTTP),
Agent.spec.declarative.tools[] with headersFrom (Secret-resolved headers
sent to the tool), and the chart-managed kagent-tool-server at
http://kagent-tools.kagent:8084/mcp. Build no MCP runtime — the gateway
RELAYS the protocol and enforces; kagent still runs the tools. (b) The
old repo's MCP-governance blueprint (plan, not code — consult, don't
port): docs/superpowers/plans/2026-08-31-tomte-p2-connectors-main-road.md
sections 7–8 in archived gambtho/tomte-old — SSRF defense set, pinned
tool snapshots, permit + proxy + projection. Record in the PR what you
took, adapted, or rejected from it.

Architecture (board + D11 + P4a precedent):
- The gateway extends the `plane/` module (P4a deviation-1 ruling) and
  reuses its Postgres, credential model (kmh_ opaque tokens, sha256-only
  storage), and ledger/audit machinery. Worker's choice whether it runs
  in the existing proxy Deployment or its own (state why; the CI node
  has ~65m CPU headroom — P4a delta — so a second pod must request ~10m).
- Seam: a Kaimahi-owned RemoteMCPServer (do NOT shadow or mutate the
  chart-managed one — P3 ruling) whose URL is the gateway; a governed
  tools agent references it via spec.declarative.tools, carrying its
  kmh_ credential in a headersFrom header from a Secret. The gateway
  authenticates it exactly like the P4a proxy authenticates chats.
- Enforcement, all fail-closed:
  - Upstream tool servers come from a committed, operator-configured
    table (the P4a upstreams pattern) — exactly one entry in this lane:
    the in-cluster kagent-tool-server. The gateway forwards nowhere
    else (that IS the egress rule at this layer; cluster-level
    NetworkPolicy is documented as a known limitation, not built here).
  - MCP scope: tools only — initialize, tools/list, tools/call. Any
    other method is denied, not relayed.
  - Per-credential tool ALLOWLIST enforced on tools/call, and PROJECTED
    on tools/list (an agent never sees a tool it cannot call). Empty or
    missing allowlist = nothing callable.
  - Every tools/call is audited to the ledger (credential, tool, status;
    denials recorded like P4a's denied rows). A failed audit write trips
    the gateway to 503 — P4a's fail-closed-degradation rule applies to
    actions exactly as it does to spend.
- Approvals/human-in-the-loop are P4c — no approval flows here beyond
  the static allowlist. No UI (D11).
- Security guidance binds: pipefail scripts for anything key-bearing,
  no key material in YAML/argv/env/logs, redaction on gateway logs, no
  redirects on keyed calls.

Deliverables:
(a) Gateway code in plane/ — `go test ./...`, gofmt, vet green at every
    commit (the go-plane CI job runs them).
(b) k8s manifests: gateway wiring, the Kaimahi RemoteMCPServer, the
    governed tools preset/patch, upstream tool-server table entry.
(c) Make targets in the P1–P4a style: govern the tools agent, set/show
    a tool allowlist, show the tool-call audit trail; `make chat
    AGENT=hello-tools` rides the gateway after governing.
(d) docs/P4B-RUNBOOK.md, including the governed-vs-ungoverned table
    updated (tool calls now governed; approvals still P4c; cluster
    NetworkPolicy egress documented as not-yet).
(e) CI, keyless, in the existing cluster job: governed tool call through
    the gateway succeeds (reuse the P3 probe-ConfigMap proof) → audit
    row asserted → a NOT-allowlisted tool call denied fail-closed and
    the denial audited. Respect the CPU ceiling (P4a delta: ~1935m/2000m
    with the plane; state your sizing).

Verification is real: live cluster evidence in the PR — the P3 probe
round-trip via the gateway, the audit rows, the denial, and proof the
agent-side wiring carries only the kmh_ token. Suite green at every
commit. Branch from current main; PR targets main; no stacked bases.
Lane ends at PR-open-with-checks-green — do not merge. Report deviations
in the PR's "Deviations & decisions" section.
```

### W6 — user documentation for shipped functionality (UNASSIGNED — paste into a fresh CLI session in this repo; runs in PARALLEL with P4b)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — process rules bind you, and
the delta sheets are your best source material. Your lane: user-facing
documentation for what is SHIPPED today — P1–P3 and P4a (governed spend).

HARD SCOPE (a P4b lane runs in parallel):
- You create NEW files under docs/ only. Do not touch README.md, the
  board, the runbooks, the Makefile, code, or CI. Do not document P4b
  (the MCP gateway) — it has not merged; tool governance is "coming",
  nothing more. If P4b merges mid-lane, still leave it out; a follow-up
  covers it.
- Branch from current main; PR targets main; no stacked bases; lane ends
  at PR-open-with-checks-green — do not merge.

Deliverables (keep it to these two files; link to runbooks rather than
duplicating them):
(a) docs/GUIDE.md — the doc for someone who just found the repo. What
    this is (one paragraph, matching the README's incubation honesty),
    zero-to-working-agent, then the concepts as a user meets them:
    agent-as-code YAML, model presets and switching, keys and how
    custody works, governing spend with `make govern` (budgets, the
    ledger, what a denial looks like). End with where to go deeper
    (runbooks per phase).
(b) docs/FAQ.md — troubleshooting and honest answers, mined from the
    delta sheets and runbooks: the small-model gotchas (ask_user
    misfires; correct tool call but wrong summary), Copilot token
    expiry and re-minting, moving from the tomte-era names (cluster,
    token path), why some presets say "schema-valid only", why ollama
    is $0 but still budgeted by tokens, what 401/403/429/503 from the
    plane each mean.

VOICE — this is half the assignment. Informal, human, direct:
- Write like you are explaining it to a colleague at their desk. "You"
  and "it". Short sentences. Contractions are fine.
- Concrete over abstract: every claim is a command someone can run or a
  thing they will see on screen.
- Be honest about rough edges the way the README and CLI-PROPOSAL are
  ("the honest case against") — say what does not work yet.
- BANNED, and reviewers will grep for them: "delve", "dive in", "dive
  deep", "leverage", "seamless(ly)", "robust", "streamline", "harness
  the power", "unlock", "supercharge", "game-changer", "In this
  guide/section, we'll", "Let's explore", "It's important to note",
  "Note that" as a sentence opener, "simply"/"just" before a step,
  "Whether you're X or Y", "In today's world", "modern" as filler,
  rhetorical-question headers, emoji in headers, bolded topic sentences
  on every bullet, and closing pep-talk paragraphs. If a sentence reads
  like a product page, delete it.
- Headers are plain nouns ("Budgets", "When the model lies about a
  tool call"), not marketing lines.

Verification is real, docs included: RUN every command you publish,
against a live cluster — YOUR OWN cluster, never the shared kaimahi-p1
(the open P4b lane owns it): `make up KIND_CLUSTER=docs-verify`, the same
override on every command you run, `make down KIND_CLUSTER=docs-verify`
when finished (published docs still show the plain commands). Paste
nothing you did not see. Where output varies (model replies),
say so instead of presenting one lucky run as typical. Cross-check every
factual claim against the current tree, not memory — presets, target
names, paths. In the PR description, list each command block and confirm
it was executed.

Report deviations in the PR's "Deviations & decisions" section.
```

### W7 — P4c: approvals / blast-radius permits (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D13, and ALL delta
sheets bind you (P4a and P4b especially, including P4b's
carried-forward items). Your lane: P4c — approvals and blast-radius
permits, the governance plane's final slice and the last arc phase.

DESIGN SOURCES FIRST (prime directive): the old repo's permit package
(server/internal/permit/permit.go in archived gambtho/tomte-old, 150
LOC) is the model to evaluate for porting: a fail-closed permit document
(DisallowUnknownFields, trailing-data rejection, deny-all is the ABSENCE
of a grant, an entry allowing nothing is an error not a deny-all) whose
mcp: connection keys were reserved until "the enforcement path exists" —
that path is now the P4b gateway. Record port/adapt/reject per pattern
in the PR. P4b's delta sheet already rules that its static allowlist is
the placeholder P4c compiles approvals into.

Model (D13 — time-boxed permits, deny-and-pend):
- A DENIED action files a pending approval request automatically (and
  `make request` can file one explicitly): a gateway tool denial files
  (credential, tool); a budget denial files (credential, budget-raise).
  Dedupe pending requests per (credential, kind, subject) — a retry loop
  must not spam the queue.
- The human decides via CLI: `make approvals` (list pending),
  `make approve ID=… [TTL=…] [USES=…]`, `make deny ID=…`. An approval
  creates a bounded GRANT — expiry by duration and/or use count, at
  least one bound REQUIRED (an unbounded grant is a config change, not
  an approval; refuse it).
- Grants COMPILE into the existing enforcement rows: a tool grant makes
  the tool pass the P4b allowlist check while live; a budget grant
  raises the effective cap while live. Expiry/exhaustion is enforced
  FAIL-CLOSED at decision time (an expired grant is simply not a grant —
  no cleanup job required for correctness; enforcement must not depend
  on a reaper having run).
- Approvals get their own audit trail (who/when/what bounds/outcome),
  same append-only + fail-closed-degradation contract as ledger and
  tool_audit. Denied-then-pended calls still write their P4b denied
  rows — approval state never suppresses enforcement audit.
- The agent experience is deny-and-retry: the denial message tells the
  operator a request was filed (`make approvals`). No held-open calls,
  no approval flows inside MCP itself.

Demo scenarios (D13, both CLI-only per D11):
(1) Tool widening: hello-tools call to k8s_get_events → denied, request
    filed → `make approve` time-boxed → call succeeds → bound expires →
    denied again. The P3 tool-server read-only posture is NOT touched.
(2) Budget overage: chat denied at the token cap → request filed →
    approve a bounded raise → chat succeeds → ledger shows the overage
    against the grant.

Deliverables:
(a) plane/ code + migrations; `go test ./...`, gofmt, vet green at every
    commit. Grant-compilation reads must be race-honest: enforcement
    evaluates grants at call time, never from a cached copy that can
    outlive expiry.
(b) Make targets above + `scripts/plane-admin.sh` subcommands, following
    the existing patterns (admin port stays off the Service; bearer
    token; input validation).
(c) docs/P4C-RUNBOOK.md per the runbook pattern; update the
    governed-vs-ungoverned tables and the README status section
    (approvals now run; the arc's governance thesis is delivered in its
    first full pass — keep the incubation framing honest about what
    remains: NetworkPolicy egress, internet-facing upstreams, richer
    approval routing).
(d) CI, keyless, in the existing cluster job: the full cycle asserted
    fail-closed for BOTH demos — denied → request filed → approve →
    allowed → expire/exhaust → denied again (use USES=1 or a short TTL
    so CI never sleeps long). Zero-ish CPU delta (extend the existing
    process; state your sizing).
(e) Small adjacent fix from the board backlog (in scope, one commit):
    guard `make up` re-pointing a governed agent at the ungoverned
    model — detect a governed modelConfig and warn + preserve (or
    re-govern), so governance doesn't silently drop off on re-runs.

Out of scope: any UI; connectors/Slack/Discord (parked candidate — P5);
approval routing to external systems; write-capable tools or any change
to the P3 tool-server posture; npm/domain/external claims.

Verification is real: live cluster evidence for both full cycles in the
PR (your own probe names and timestamps), plus proof expiry re-denies.
Suite green at every commit. Branch from current main; PR targets main;
no stacked bases. Lane ends at PR-open-with-checks-green — do not
merge. Report deviations in the PR's "Deviations & decisions" section.
```

### W8 — P5a: governed Slack connector, the demo that makes governance legible (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D14, and ALL delta
sheets bind you (P3, P4b and P4c especially). Your lane: P5a — a
governed Slack outbound path, and the demo that makes the whole
governance arc legible.

WHY THIS LANE EXISTS, keep it in view: every control built so far
protects an agent that lists ConfigMaps — nothing in the current demo
needs governance. Posting to a channel humans read is the first
genuinely consequential action in this repo. Your deliverable is NOT
"Slack works". It is: an agent tries to post, is DENIED, a request is
filed, a human grants a bounded approval, the message lands, the use is
burned, the next attempt is denied again — and the audit trail shows
every step. The connector is the payload; the approval gate is the
point.

SURVEY FIRST (prime directive): Slack MCP servers already exist. Deploy
one through kagent's own CRDs (MCPServer for a stdio/npx server, or
RemoteMCPServer) — write NO connector code. Record in the PR: what you
surveyed, which server you chose, and its provenance and pinning. You
are introducing third-party code that will hold a workspace token —
pin it by version or digest, say why you trust it, and treat that
judgement as part of the deliverable.

Architecture:
- The Slack MCP server runs in-cluster, deployed by kagent, with the
  bot token mounted to IT as a Secret — never to the agent, never in
  YAML, argv, env listings, or logs. Capture it stdin-only via a
  pipefail script (scripts/plane-secrets.sh and copilot-secret.sh are
  the precedent). Evaluate whether custody instead belongs with the
  plane (gateway-injected, P4a-style) and state your choice: pick the
  simplest option that keeps the token off the agent, and justify it.
- The agent reaches Slack THROUGH the P4b gateway; the gateway's
  upstream table gains the in-cluster Slack MCP server as a second
  entry. Document plainly: the gateway's upstreams remain in-cluster
  (so the P4b ruling deferring the SSRF/hardened-dialer set still
  holds), but the Slack server pod is the FIRST component in this repo
  with deliberate INTERNET egress. That makes it the strongest argument
  yet for the still-unbuilt NetworkPolicy work — which stays out of
  scope here but must be named honestly in the runbook.
- Posting is NOT allowlisted by default; it is the approved action.
  Read-only Slack tools (channel list, history) may be allowlisted from
  the start if the survey shows it helps the story.
- The demo agent runs the GOVERNED Copilot preset (D14) so one demo
  exercises spend governance and tool governance together, and so the
  model can actually compose a message and call a tool — qwen2.5:3b is
  documented doing neither reliably. CI stays KEYLESS on ollama: no
  Slack token and no Copilot token in CI, ever (public, fork-exposed).

OUTWARD-FACING CONSTRAINT (board rule): posting to Slack sends messages
real people can read. Post ONLY to a private test channel the user has
named for this purpose. Never a shared, public, or team channel. If no
channel has been designated when you reach that step, STOP and ask —
do not choose one yourself.

Deliverables:
(a) Manifests: the Slack MCP server, its gateway upstream entry, the
    governed wiring — following existing k8s/ patterns.
(b) Make targets in the established style (stdin-only token capture,
    govern the Slack path, run the demo) and a documented end-to-end
    demo sequence someone can follow live.
(c) docs/P5A-RUNBOOK.md, with the governed-vs-ungoverned table updated
    and an honest statement of what the internet-egress pod means.
(d) CI, keyless: assert everything that does NOT need a Slack token —
    manifests valid against live CRDs, gateway upstream table and tool
    projection, and the deny → file → approve → allow → exhaust cycle
    against a stubbed or in-cluster stand-in. State explicitly in the
    PR which parts of the Slack path CI can and cannot cover; do not
    let a stand-in imply the real path is CI-verified.
(e) README status touch only if needed; keep the incubation framing.

Out of scope: inbound/webhooks (P6), NetworkPolicy egress, AKS (P5b, a
separate lane), any UI, npm/domain/external claims.

Verification is real: the PR must show the full demo — the denial, the
filed request, the bounded approval, the message actually landing (a
screenshot or permalink is fine; redact anything workspace-identifying
you would not want in a public repo), the burned use, the re-denial,
and the plane's audit trail for all of it. Suite green at every commit.
Branch from current main; PR targets main; no stacked bases. Lane ends
at PR-open-with-checks-green — do not merge. Report deviations in the
PR's "Deviations & decisions" section.
```

### W9 — P5b: cluster portability + a real AKS run (UNASSIGNED — paste into a fresh CLI session in this repo)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D15, and ALL delta
sheets bind you. Your lane: P5b — make the stack cluster-agnostic and
prove it by running the governance plane on a real AKS cluster.

WHY THIS LANE EXISTS: the README has named AKS as the managed target
since D6, and nothing has ever run there. Worse, the tooling cannot even
target it — `KUBE_CTX := kind-$(KIND_CLUSTER)` prefixes every context
with `kind-`. This lane closes a claim the project has been making in
public.

Verified obstacles (checked by the coordinator; confirm each yourself):
- `KUBE_CTX := kind-$(KIND_CLUSTER)` hardcodes the kind context prefix.
- `make cluster` unconditionally runs `kind create cluster`.
- The plane image is built locally and `kind load`ed, and
  k8s/plane/proxy.yaml pins `imagePullPolicy: Never` — deliberate for
  kind (P4b deviation 6), unusable on AKS. This is the real work: a
  registry path.
- SOFTER THAN EXPECTED: the Postgres PVC sets no `storageClassName`, so
  it should take AKS's default (managed-csi). Verify rather than assume.
- The CI-only Agent-resource shrink patch must not leak into the AKS
  path (it exists for a 2-CPU runner).

Shape (D15):
- Registry: a PRIVATE ACR — `az acr build` (build in Azure, no local
  docker push) and `az aks update --attach-acr`. Do NOT publish a public
  image: that is outward-facing and a soft public claim on a provisional
  name while D9's gates are open. `imagePullPolicy` becomes
  environment-dependent: `Never` stays correct for kind (keep its
  rationale comment), a pull policy for AKS.
- Cluster lifecycle: YOU create the AKS cluster with the already
  authenticated `az` CLI and YOU TEAR IT DOWN at lane end — teardown is
  mandatory, not best-effort, and the PR must state the cluster is gone
  plus a rough spend estimate. Pick a cheap SKU/region and say why. Ship
  the provisioning as a documented, parameterised script.
- Model: Copilot-only on AKS. Do NOT deploy Ollama there (the keyless
  path is CI-proven on kind every PR). The AKS run uses the governed
  Copilot preset, so it exercises spend governance and tool governance
  on the managed cluster.
- Slack (P5a) stays OUT of the AKS run: putting a real workspace token
  into a temporary cloud cluster is credential exposure for little added
  proof. The wiring is plain CRDs and a gateway upstream entry — say so
  in the runbook, don't demonstrate it.

TWO HARD GUARDRAILS:
1. NO AZURE IDENTIFIERS IN COMMITTED FILES OR THE PR — no subscription
   ID, tenant ID, resource-group name, ACR login server, or cluster FQDN.
   Parameterise them (env vars/placeholders) and redact them from pasted
   evidence. This repo is public.
2. CONTEXT SAFETY. Once the tooling can target non-kind clusters, a
   mistyped `make down` or `make up` can hit the wrong cluster — the
   repo's own CLI-PROPOSAL names this foot-gun ("--apply on a production
   context by accident"). Every target that MUTATES must print the
   target context and namespace, and must require explicit confirmation
   when the context is not a local kind cluster. Destructive targets
   (`down`) especially. Fail closed: no confirmation, no action.

Deliverables:
(a) The portability refactor — kind and AKS both first-class, kind's
    behaviour UNCHANGED for existing users (this is the main regression
    risk; CI is your proof).
(b) The ACR/AKS provisioning + deploy path as parameterised scripts and
    make targets, in the established style.
(c) docs/P5B-RUNBOOK.md: the exact commands from an empty Azure
    subscription to a governed chat on AKS, the cost note, teardown, and
    an honest list of what differs from kind.
(d) CI: stays on kind, keyless, and MUST still pass unchanged — plus any
    cheap static assertion of the portability work (e.g. the context
    guard's logic). No Azure credentials in CI, ever.
(e) README/status: AKS moves from claimed to demonstrated, with the
    honest scope (one verified run, then torn down — not a maintained
    environment).

Out of scope: inbound/webhooks (P6), NetworkPolicy egress, Slack on AKS,
any UI, npm/domain/public-image claims, Azure Database for PostgreSQL
(D11 says in-cluster Postgres).

Verification is real: PR evidence of the governed stack running on the
actual AKS cluster — plane deployed, governed Copilot chat completing,
a ledger row, a budget denial, and the tool path working — with Azure
identifiers redacted, PLUS proof the kind path still works end to end.
Suite green at every commit. Branch from current main; PR targets main;
no stacked bases. Lane ends at PR-open-with-checks-green — do not merge.
Report deviations in the PR's "Deviations & decisions" section.
```

## Post-move follow-up (D16)

- `plane/go.mod` is `module github.com/gambtho/kaimahi/plane` with 36
  imports of that path. Nothing fetches it (internal module), so builds are
  unaffected — but the canonical path should become
  `github.com/kaimahi-agents/kaimahi/plane`. Mechanical sed across plane/,
  SEQUENCED AFTER #23/#24 merge (both touch plane/; doing it now would
  conflict with #24 everywhere). Also `docs/CLI-PROPOSAL.md`'s
  `npx github:gambtho/kaimahi` and NAMING.md's present-tense owner lines.
  Small coordinator PR when the lanes are in.

## CI flake class 2 — model relaying (recorded 2026-09-01) — RESOLVED by #29 (W14)

PR #24's e2e went red at the P3 probe step with the tool call SUCCEEDING
(function_call + isError:false; the tool's own output contained
`probe-46649d55`) while the 3B model relayed it as `probe-466448a247`, and
`scripts/verify-chat.py` requires the probe name in the model's REPLY.
That is the P3-delta relaying-side failure mode, now observed in CI; the
system-message mitigation measured 10/10 at the time but is not 100%.
Independent of the transport flake #20 fixed. Follow-up (small, not GO
until the parallel set merges — it touches CI): the verifier should
take the probe name from the `function_response` payload, which is the
real proof of a live round-trip, and treat the prose as informational.
Requiring a 3B model to copy an unguessable string verbatim tests the
model, not the tool path. Until then: re-run the job when this shape
appears; do not hold lanes for it.

## CI flake class 3 — the old pod answers after `use` (recorded 2026-09-01)

Docs-only board PR #27 went red at "Assert the ledger recorded the
governed chat": the governed chat COMPLETED, the ledger had zero rows.
`make govern` delegates to `use`, which patches the ModelConfig, waits
for `rollout status` and the Agent's Ready condition, then returns — with
`maxSurge: 1, maxUnavailable: 0` that is the moment the NEW pod is Ready
while the OLD one (still on the ungoverned preset) is terminating. If the
chat lands on the old pod it completes straight against ollama and nothing
is metered. Hypothesis: the failed attempt's log was replaced by the
re-run (which passed), and a silent ledger-write failure is ruled out by
P4a's design (a failed write trips the proxy to 503, so the chat could
not have completed). Follow-up (Makefile, small, NOT GO yet): `use` should
return only when exactly one pod with the new template hash remains, so
"governed" means the ungoverned pod is gone, not outnumbered. Bundle with
the Makefile comment for `AKS_NETWORK_POLICY` (W15 deviation 3).

## Open items after the second parallel set (2026-09-01)

- **#16 (Tatsinnit's CLI)** — rulings given on the PR; blocking fix is the
  uncommitted `cli/bin/kaimahi.js` (root `.gitignore` `bin/`). Waiting on
  the author.
- **D9 naming gates** — cultural read + trademark counsel, now more urgent
  (org exists; npm publish deferred until they clear).
- **Makefile micro-lane**: `use` waits for the old pod to be gone (flake
  class 3); `AKS_NETWORK_POLICY` in the AKS variable comment block.
- **Unverified engines**: `azure`/`calico` on AKS; multi-node AKS with the
  probe's single-node caveat.
- **P8 candidates** (not GO): Slack Events live end to end (needs a public
  ingress); approval routing + per-approver identity; shared limiter/queue
  for a multi-replica plane; internet-facing gateway upstreams with the
  hardened dialer/SSRF set; retiring the phase-runbook stubs.

## Parallel set rules (P7a / P7b / P7c, 2026-09-01)

Three lanes run at once by user ruling. The board's "one session owns a
contended directory" rule is relaxed deliberately, so the boundaries are
explicit instead:

1. **Branch from a main that contains PR #20** (the CI flake fix). It
   landed standalone precisely so no lane inherits another's red CI.
2. **Own your own cluster.** `KIND_CLUSTER=netpol-verify` (P7a),
   `KIND_CLUSTER=inbound-verify` (P7b). NEVER touch `kaimahi-p1` — it is
   the demo cluster. P7c needs no cluster. This rule already cost us once
   (W6↔P4b) and nearly again (P5b's probe aimed at AKS).
3. **`docs/` structure belongs to P7c.** P7a and P7b each write ONE
   user-facing file named for its CAPABILITY, not its phase — e.g.
   `docs/egress.md`, `docs/inbound.md` — and change no other doc. P7c
   owns the index, the naming scheme, and every existing file.
4. **Expect textual conflicts in `Makefile` and `ci.yml`** between P7a and
   P7b; both append. They are cheap. Whoever merges second rebases —
   check the PR state before every push (standing rule, earned three
   times).
5. Everything else unchanged: survey first, verification is real, suite
   green at every commit, lane ends at PR-open-with-checks-green.

### W10 — P7a: NetworkPolicy egress (UNASSIGNED — paste into a fresh CLI session)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D15, ALL delta sheets,
and the PARALLEL SET RULES bind you. Your lane: P7a — NetworkPolicy
egress.

WHY THIS LANE MATTERS: the board defines this product as "budgets and
spend metering, approval workflows and blast-radius permits, credential
custody, EGRESS ENFORCEMENT, and audit". Every clause now runs and is
CI-asserted except egress enforcement. P5a made the gap concrete by
putting a deliberately internet-reaching pod (the Slack MCP server) in
the cluster; today's blast radius is bounded by the gateway allowlist,
the server's channel-ID restriction and Slack's own scopes — three real
layers, none of them a network boundary. You are building the boundary.

SURVEY FIRST: NetworkPolicy is a Kubernetes primitive and kind's default
CNI (kindnet) supports it — VERIFY that on your cluster before relying
on it, because a NetworkPolicy that is silently unenforced is worse than
none (it reads as protection). Check what kagent's chart already ships.
Build no policy engine; write policy.

Shape:
- Default-deny egress and ingress in the `kaimahi` namespace, then
  allow exactly what the delta sheets say must work: the proxy to its
  Postgres, the gateway to the in-cluster tool servers, the proxy to its
  configured LLM upstreams (note: on kind the governed path is in-cluster
  ollama; the Copilot path is internet-bound — handle both honestly),
  kagent's controller to the gateway, and DNS. Deny everything else.
- The Slack MCP server pod is the one component that legitimately needs
  the internet. Allow it deliberately and narrowly, and say in the doc
  what that allowance does and does not constrain (egress IP/port policy
  is not a URL allowlist — be precise about the residual gap).
- FAIL CLOSED and PROVE IT: the deliverable is not "policies exist", it
  is "a pod that should not reach X demonstrably cannot". Assert a
  NEGATIVE — exec into a pod and show a blocked connection timing out /
  refused — alongside the positive that everything still works.
- Do not break P1–P5: `make up`, chat, tools, governance, approvals must
  all still pass on your own cluster and in CI.

Deliverables: policy manifests in k8s/; make/CI wiring; ONE doc file
`docs/egress.md` (capability-named — P7c owns docs structure, see the
parallel rules); CI assertions in the existing keyless job, including
the negative test. Mind the CPU/CI budget (P4a/P4b deltas).

Out of scope: P6 inbound (a parallel lane), docs restructure (parallel
lane), AKS-specific policy beyond noting portability, any UI.

Verification is real: your own probe names and timestamps, positives AND
the blocked negative, on KIND_CLUSTER=netpol-verify. Branch from a main
containing PR #20; PR targets main; no stacked bases; lane ends at
PR-open-with-checks-green — do not merge. Report deviations in the PR.
```

### W11 — P7b: inbound connectors (UNASSIGNED — paste into a fresh CLI session)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — prime directive, process
rules, security standing guidance, decisions D1–D15, ALL delta sheets,
and the PARALLEL SET RULES bind you. Your lane: P7b — inbound
connectors: letting the outside world TRIGGER an agent, governed.

This is the larger lane and the one genuine net-new surface left. P5a
gave agents a governed way to ACT on the world; this gives the world a
governed way to act on agents.

SURVEY FIRST (prime directive, and be rigorous — this is where net-new
code is most tempting): kagent agents already expose A2A endpoints, and
the kagent CLI already invokes them. Establish and record whether
anything upstream already bridges an external event to an A2A invoke
(kagent itself, its CRDs, any MCP or A2A tooling). Only build the
bridge the survey proves is missing, and justify every file.

Shape:
- The bridge extends the `plane/` module (P4a deviation-1 precedent) and
  reuses what already exists — do NOT grow a second auth system: inbound
  callers authenticate with the plane's `kmh_` opaque credentials,
  sha256-only storage, exactly as the proxy and gateway do.
- Every inbound event CAUSES SPEND, so it must sit behind P4a budgets
  and be ledgered/audited like any other governed action. An event that
  cannot be recorded must not be honoured (the fail-closed-degradation
  rule).
- It is an INGRESS surface — the first in this repo — so treat these as
  first-class requirements, not polish: authentication before any work,
  replay protection, request size limits, rate limiting, and a bounded
  queue. Reject rather than buffer without bound. Signature verification
  where a source offers it (e.g. HMAC) beats a bearer token; support it
  where it exists.
- Scope the sources deliberately: a generic authenticated webhook is the
  primitive. Do at most ONE named real source end to end and say why.
  Slack Events (closing the P5a loop) is the obvious candidate but needs
  a public URL — if that is not reachable from a kind cluster, say so and
  demonstrate the generic path rather than faking it.
- Approvals: an inbound trigger is consequential. State clearly whether
  triggering is itself an approvable action (P4c) or gated only by
  credential + budget, and justify it.

Deliverables: plane/ code + migrations (go test/gofmt/vet green at every
commit); manifests; make targets in the established style; ONE doc file
`docs/inbound.md` (capability-named — P7c owns docs structure); keyless
CI asserting the full path fail-closed, including a rejected
unauthenticated event and a rejected replay.

Out of scope: NetworkPolicy (parallel lane), docs restructure (parallel
lane), any UI, npm/domain claims, outbound connectors beyond what a
demo needs.

Verification is real: your own probes and timestamps on
KIND_CLUSTER=inbound-verify. Branch from a main containing PR #20; PR
targets main; no stacked bases; lane ends at PR-open-with-checks-green
— do not merge. Report deviations in the PR.
```

### W12 — P7c: docs restructure (UNASSIGNED — paste into a fresh CLI session)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — process rules and the
PARALLEL SET RULES bind you. Your lane: P7c — restructure the
documentation around what a reader wants to DO, not the order we
happened to build it.

THE PROBLEM, stated by the user: "i don't think having a series of
sequential runbooks makes sense." There are eight phase-named runbooks
(P1, P2, P3, P4A, P4B, P4C, P5A, P5B ≈ 1,700 lines) plus GUIDE.md and
FAQ.md. `P4B-RUNBOOK.md` is a coordination artifact leaking into user
documentation: a reader cannot tell what it contains, and the only way
to find "how do I govern tool calls" is to read all eight. Measured:
setup instructions barely repeat across them, so this is a NAVIGATION
and NAMING problem, not a duplication problem — do not "solve" it by
mass-deleting content.

Your thesis: reorganise by CAPABILITY, with one obvious entry point and
names that say what they are about. Propose the structure IN THE PR and
justify it. A likely shape (yours to argue with): a single entry doc
that routes; capability docs (getting started, models and endpoints,
tools, governing spend, governing tool calls, approvals, connectors,
running on a managed cluster); FAQ kept as-is (it works); the phase
runbooks' content redistributed.

The editorial call that matters most: each runbook mixes HOW TO USE a
capability with WHY IT IS THIS WAY and WHAT WE VERIFIED. The board's
delta sheets already hold the verification record. Decide deliberately
what stays user-facing (caveats, gotchas, limitations — these are the
best writing in the repo and must NOT be lost) versus what is
historical, and state your rule. Losing the honest caveats would make
these docs worse while making them prettier.

Constraints:
- docs/COORDINATION.md is coordinator-owned — DO NOT TOUCH IT.
- Two build lanes run in parallel and will each add ONE capability-named
  file (`docs/egress.md`, `docs/inbound.md`). Leave room for them in your
  structure; do not depend on their content.
- Preserve every honest limitation and "not live-verified" marker. The
  repo's credibility rests on them.
- Keep the voice: informal, direct, concrete, no marketing register. The
  banned-phrase list from the W6 lane still applies ("delve", "seamless",
  "robust", "leverage", "In this guide we'll", "simply", rhetorical
  headers, closing pep-talks).
- Update README links and any cross-references you break. A broken link
  is a failed lane.
- Redirects//tombstones: decide whether removed filenames need a stub
  pointing at the new location (external links may exist) and say why.

Verification: every internal link resolves (check them mechanically);
every command you carry forward is still accurate against the current
tree — if you cannot verify a command, mark it rather than deleting it.
No cluster needed; if you want to verify commands live, use your OWN
cluster, never kaimahi-p1.

Branch from a main containing PR #20; PR targets main; no stacked bases;
lane ends at PR-open-with-checks-green — do not merge. Report deviations
in the PR.
```

### W13 — post-move: Go module path and owner references (UNASSIGNED — paste into a fresh CLI session ONLY AFTER PRs #23 and #24 have merged)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — process rules, decision D16,
and the security standing guidance bind you. Your lane: the mechanical
follow-up to the repo's move into the kaimahi-agents organization.

SEQUENCING IS THE WHOLE RISK HERE. Before doing anything, confirm with
`gh pr view 24 -R kaimahi-agents/kaimahi --json state` (and #23) that both
are MERGED, and branch from a main that contains them. #24 touches
plane/ everywhere; a module-path rename underneath it would conflict in
every file. If either is still open, STOP and say so — do not start.

What changes (and nothing else):
1. `plane/go.mod`: `module github.com/gambtho/kaimahi/plane` becomes
   `module github.com/kaimahi-agents/kaimahi/plane`, and every import of
   the old path across plane/ (there were 36 at last count; recount after
   #24) is rewritten. Nothing fetches this module — it is internal — so
   this is canonical hygiene, not a build fix. `go.sum` should not change;
   if it does, explain why.
2. Owner references in docs that speak in the PRESENT tense:
   `docs/CLI-PROPOSAL.md` (`npx github:gambtho/kaimahi` → the org) and
   `docs/NAMING.md` (the "GitHub redirects from the old paths are active"
   line and the repo-history paragraph gain the org move as D16).
   Historical mentions — D5/D10 quotes, the gambtho/tomte-old archive
   links — stay exactly as they are; they are history.
3. `docs/NAMING.md` says "Nothing here is claimed." That is no longer
   strictly true: a GitHub organization named for the project now exists.
   Update it factually, in the doc's own plain voice — an org name is a
   public claim on the still-provisional name, and D9's two gates
   (cultural read, trademark counsel) remain open and are now more
   urgent. Do not editorialise beyond that; the ruling is the user's.
4. Then a full audit: `git grep -n gambtho` over the tree. Every
   surviving hit must be historical (tomte-old links, quoted decisions)
   and listed in the PR with its justification. docs/COORDINATION.md is
   coordinator-owned — DO NOT TOUCH IT; list its hits as "board, excluded".

Do NOT rename anything else: image names (`kaimahi-proxy`), namespaces,
Secrets, make targets, cluster names and package names are unchanged.
This lane renames an import path and fixes owner strings, nothing more.

Verification is real:
- `cd plane && gofmt -l . && go vet ./... && go build ./... && go test ./...`
  all clean/green.
- Build the plane image locally (`make plane-image` or the Dockerfile
  directly) — the Docker build is the one place a module path can bite
  that `go build` on the host would not show. No cluster needed.
- `bash scripts/check-no-azure-ids.sh` and
  `python3 scripts/check-doc-links.py` clean.
- CI is expected to pass unchanged; if the go-plane job needs any edit,
  that is a deviation to report, not a silent fix.

Branch from current main (post-#23/#24); PR targets main; no stacked
bases; lane ends at PR-open-with-checks-green — do not merge. Report
deviations and the gambtho audit table in the PR.
```

### W14 — CI hygiene: a verifier that proves the tool path, and a docs-only short-circuit (UNASSIGNED — paste into a fresh CLI session)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — process rules and the
"CI flake class 2" note bind you. Your lane: two CI fixes, one PR. You
own .github/workflows/ci.yml and scripts/verify-chat.py and nothing else;
three other lanes are running in parallel and touch neither file.

1. verify-chat.py currently requires the probe ConfigMap's name in the
   MODEL'S REPLY. A 3B model garbles unguessable strings (PR #24 went red
   on exactly that: the function_response contained `probe-46649d55`, the
   prose said `probe-466448a247`). That assertion tests the model, not
   the tool path. Change it: the probe name must appear in the
   function_response payload — the actual proof of a live round-trip —
   with function_call + isError:false as now. Print the prose; do not
   assert on it. Keep the non-empty-reply assertion for plain chats.
   Verify with fixtures: PR #24's failing task JSON (run 33562345538)
   must now PASS; a task with no function_response must FAIL; a task
   whose function_response lacks the probe must FAIL.

2. Board updates are now PRs (D17) and the ruleset requires the e2e
   check, so a docs-only PR waits ~11 minutes for a cluster it cannot
   affect. Add a docs-only short-circuit: the e2e job still RUNS and
   REPORTS success (a required check that never reports blocks the
   merge), but skips the cluster steps when every changed file is
   documentation (docs/**, *.md). FAIL CLOSED: if the changed-file set
   cannot be determined (shallow history, force push, missing base), or
   contains anything else at all, run the full job. Use plain git, no new
   marketplace actions. Cover both pull_request and push-to-main events.

Verification is real, on this PR's own runs: push the docs-only commit
FIRST (a comment-only change in ci.yml does not count — make the first
commit touch only a doc) and show the e2e check green in seconds; then
the code commit and show the full e2e run. Both runs linked in the PR.

Branch from current main; PR targets main; no stacked bases; lane ends
at PR-open-with-checks-green — do not merge. Report deviations in the PR.
```

### W15 — AKS actually enforces NetworkPolicy (UNASSIGNED — paste into a fresh CLI session)

```
You are a worker session for the Kaimahi project (repo root: this
checkout). Read docs/COORDINATION.md first — decisions D15/D16, the P5b
and P7a delta sheets, and the security standing guidance bind you. Your
lane: close the gap P7a found — AKS does not enforce NetworkPolicy by
default, so the plane's policies would be present and inert there,
which is the "worse than none" case. You own scripts/aks-up.sh,
docs/aks.md and docs/egress.md; three other lanes run in parallel.

Do:
- Make the provisioning script create clusters WITH policy enforcement
  (`az aks create --network-policy …`; choose azure vs cilium, say why,
  keep it parameterised). Existing clusters are not migrated — say so.
- Prove it on a real cluster, the P5b way: you create it with the
  already-authenticated az CLI, deploy the plane (TARGET=aks is
  Copilot-only per D15, so mint the Copilot secret first — P5b delta),
  run `TARGET=aks make netpol-verify`, and the probe must report the
  boundary enforced — including the unlabeled-pod row, which is the
  enforcement check itself. If AKS is multi-node, handle the probe's
  documented single-node caveat honestly rather than skipping the row.
- TEAR THE CLUSTER DOWN at lane end. Mandatory. State it is gone and
  give a rough spend figure.
- Update docs/aks.md and the "AKS: not exercised" lines in
  docs/egress.md to what actually happened.

Guardrails, both hard: NO Azure identifiers in committed files or the
PR (subscription, tenant, resource group, ACR host, cluster FQDN —
scripts/check-no-azure-ids.sh is the gate); and every mutating command
goes through the context guard.

Out of scope: everything else. No Azure credentials in CI, ever.

Verification is real: the probe's full output from the AKS cluster,
redacted; the teardown; the spend note. Branch from current main; PR
targets main; no stacked bases; lane ends at PR-open-with-checks-green
— do not merge. Report deviations in the PR.
```

## Delta sheets from finished lanes

### W13 — post-move Go module path + owner references (PR #26, merged 2026-09-01)

`plane/go.mod` is `module github.com/kaimahi-agents/kaimahi/plane`; every
import rewritten; `go.sum` unchanged; go-plane CI needed no edit.
CLI-PROPOSAL's `npx github:` path and NAMING.md's present-tense lines
updated; NAMING.md now says plainly that one thing IS claimed — the
`kaimahi-agents` organization — and that D9's gates are more urgent for
it. Coordinator verification: module line confirmed on main; `git grep
gambtho` outside the board finds only NAMING.md's history paragraphs and
the tomte-old archive links. No deviations. Accepted.

### W14 — CI hygiene (PR #29, merged 2026-09-01)

`scripts/verify-chat.py` now requires the probe inside the successful
`function_response` payload and prints the prose without asserting on it;
self-test fixtures cover PR #24's real garbled case (passes), no
function_response (fails), payload without the probe (fails). The e2e
job classifies each PR (base tip vs merge commit, fetched by SHA under
`persist-credentials: false`) and skips the cluster steps when the diff
is docs-only, FAILING CLOSED to a full run on any doubt; hygiene gains a
self-check that every e2e step (37) carries the guard, so a future step
cannot silently escape it. Coordinator verification: verifier and guard
read on main; the run on the PR proved the classify step on a real
`pull_request` event (`docs_only=false`, because it changed ci.yml).
Deviation accepted: the "docs-only commit first" demo the prompt asked
for is impossible on a PR that edits the workflow, and per-push
classification would be fail-open — the per-PR semantics are the right
ones. THIS board PR is the first live docs-only test. Docs follow-up
(docs/tools.md's old wording) done in this PR.

### W15 — AKS provisions NetworkPolicy enforcement (PR #30, merged 2026-09-01)

`scripts/aks-up.sh` always provisions a policy engine —
`AKS_NETWORK_POLICY=cilium` by default (Azure CNI Overlay + Cilium,
Microsoft's recommendation), `azure`/`calico` accepted but unverified;
existing clusters are never migrated, and an explicit empty value is
refused. Proven on a real cluster: `TARGET=aks make netpol-verify` reported
the boundary enforced with the unlabeled-pod row blocked on every column
(ollama column skipped by design — Copilot-only target, D15); cluster
torn down; spend recorded in docs/aks.md. Coordinator verification:
`az group list` shows no kaimahi resource group; identifier scan clean on
main; flag and default read in the script. Rulings — accepted: the
AcrPull confirmation now asks ARM directly (the tenant's conditional
access refused the CLI a Graph token while ARM worked) and resolves the
role definition by NAME at runtime, since its GUID would rightly trip the
identifier gate; the post-run review round's two fixes (the cluster
existence probe failed OPEN — any az error read as "does not exist" and
fell through to a create that would have PUT a new network profile onto
an existing cluster; and `${VAR:-}` silently defaulting an explicit
empty) are exactly the fail-closed discipline this board asks for.
Carried forward: Makefile comment for `AKS_NETWORK_POLICY`; azure/calico
and multi-node unverified.

### P7b — inbound connectors (PR #24, merged 2026-09-01)

Delivered on main: `plane/internal/inbound` — a webhook → A2A bridge in
the existing proxy process; hooks from a committed, secret-free table;
per-hook HMAC (Slack-style signed timestamp + delivery id) or bearer
auth; a signed-timestamp replay window (±5 min) plus a delivery-id
index (replay → 409); per-hook token-bucket rate limit and request-size
cap BEFORE authentication (bounds the audit-write rate an
unauthenticated flood could cause — a deliberate, stated trade); the
target agent's governed budget checked at the door (429, no grant use
burned); a bounded `inbound` grant consumed per admitted event (403 +
auto-filed request when none is live); one bounded queue of
invocations; audit for every outcome with the fail-closed-degradation
rule (503 while the trail cannot be written). Migration widens the
approvals `kind` CHECK to `'inbound'` rather than adding a table. The
proxy's one new egress (kagent-controller:8083) added to the P7a policy.
`docs/inbound.md` is the capability doc.

Coordinator verification (2026-09-01): the worker tore down
`inbound-verify` at lane end, so no live reproduction; verified instead
by (a) the CI matrix on the merge commit and again on main's post-merge
run (green): hook table in-cluster and secret-free; admin not on a
Service; unauthenticated / forged / stale-timestamp → 401 ×3 audited;
exhausted target budget → 429 at the door; signed-but-ungranted → 403
with a request filed; USES=2 approval admits and the agent runs (outcome
row with runtime token counts, one more governed ledger row); replay →
409; exhaustion → 403; and (b) a read of the gate order in
`inbound.go`: limiter → size cap → authenticate → budget credential →
budget door → grant → queue → audit, with the audit breaker now BEFORE
authentication (the lane's own polish pass found it had sat after —
"nothing is honoured while degraded" is now true of the code, not just
the doc).

The one red run (twice) was diagnosed by the coordinator: first the P3
relaying flake (not the lane's), then the lane's own ungranted-event
probe expecting 403 but getting 429 because the earlier P4c
budget-overage step had exhausted the cap. The worker chose "budget
first, like P4a" and asserted THAT order in CI (b47d682) — accepted: a
budget denial is the cheaper answer and never burns a grant use.

Rulings — all ACCEPTED: rate limit before auth (stated trade); in-memory
limiter and queue (single replica, documented); session attribution to
the hook not the external sender; kind-CHECK widening over a new table;
image tag → p7b. Slack Events end to end is NOT covered (no public URL
in CI) — the generic signed webhook is what is proven; the doc says so.

Carried forward: a multi-replica plane needs a shared limiter/queue;
Slack Events live needs a public ingress (P8 candidate alongside
approval routing).

### P7a — NetworkPolicy egress (PR #23, merged 2026-09-01) — the product sentence is now complete

Delivered on main: `k8s/plane/network-policy.yaml` — default-deny
Ingress+Egress on the `kaimahi` namespace; proxy admits the `kagent`
namespace on 8080/8081 and reaches only Postgres 5432, ollama 11434,
kagent-tools 8084, the Slack MCP server 13080 and DNS; Postgres admits
the proxy only and has EXPLICIT ZERO egress; the Slack MCP server admits
the proxy only and may reach DNS plus TCP 443 to non-private addresses
(0.0.0.0/0 minus the six private/link-local/CGNAT ranges). The proxy's
own 443 allowance for Copilot is OPT-IN (`k8s/egress-copilot.yaml`,
applied by `plane-copilot-secret`, removed by `egress-copilot-off`,
kept outside `k8s/plane/` so kind and CI deploy an internet-free proxy).
`scripts/netpol-probe.sh` / `make netpol-verify` is the proof; CI runs a
parsed shape check in hygiene and the probe after every governed e2e
step, so every P4a–P5a assertion now runs WITH the policies in place.
`docs/egress.md` is the capability doc. Ships with `make plane` on both
targets — the boundary is never a separate step.

Coordinator verification (independent, 2026-09-01, post-merge): ran
`make netpol-verify` myself on the lane's `netpol-verify` cluster —
control pod reaches everything but Postgres; an unlabeled pod in
`kaimahi` reaches NOTHING (the enforcement check itself); proxy-shaped
reaches DNS/ollama/Postgres and is blocked from the internet on 443 and
80; slack-shaped reaches DNS and 443 only; the REAL Postgres pod, exec'd,
reaches its own loopback and nothing else. `netpol-probe: boundary
enforced as written`. kindnetd v20250214 logs `Starting controller
kube-network-policies`. Main CI green on the merge commit. Manifests
read line by line against the design above.

Rulings — all ACCEPTED: Copilot egress opt-in rather than baked in (a
permanent 443-out would make "internet-free proxy" false on every kind
cluster and untestable); proxy ingress keyed on the `kagent` NAMESPACE
rather than pod labels kagent may rename (the seam authenticates by
credential; the network's job is "only from where agents live"); the
~1–2 s unpoliced window for brand-new pods on kind measured and
documented as a residual (real plane workloads are long-lived); the
`kagent` and `ollama` namespaces left unpoliced and said so; IPv4-only.
The lane's own review round (a `to`-less rule allows everything and
previously passed the shape check; the exec'd-pod row needed a loopback
positive so a `nc`-less image cannot pass as "blocked") is the
fail-closed discipline this board asks for, applied to the verifier.

Findings for the record:
- **The Slack direct-access bypass P5a measured is now closed by the
  network** — the MCP server accepts connections from the proxy only.
- **AKS does not enforce NetworkPolicy by default.** `aks-up.sh` passes
  no `--network-policy`; on such a cluster these policies are present
  and inert — exactly the "worse than none" case. `TARGET=aks make
  netpol-verify` fails loudly with "NOT ENFORCED". Fixing the
  provisioning flag is a small follow-up for the next AKS run.
- **An IP/port rule is not a URL allowlist**: the Slack pod may still TLS
  to any public host on 443, bounded by the server's code and P5a's
  three non-network layers. Closing that needs FQDN policy or an egress
  gateway; `docs/egress.md` says so plainly.

Reconciliation owed (coordinator PR, AFTER #24 merges to avoid k8s/plane
conflicts): lines that now say NetworkPolicy is unbuilt — `README.md`
(the incubation banner and line ~210), `docs/README.md` lines ~35 and
the "Not built" table row, `docs/slack.md` ~46, and the comments in
`k8s/plane/upstreams.yaml` and `k8s/slack-mcp.yaml`. Plus the AKS
`--network-policy` provisioning flag.

### P7c — docs restructure by capability (PRs #21/#22, merged 2026-09-01)

Delivered on main: `docs/README.md` as the router ("by what you want to
do" table, the ONE governed-vs-ungoverned table, the editorial rule
stated in the open); capability docs `getting-started`, `models`,
`tools`, `spend`, `tool-governance`, `approvals`, `slack`, `aks`; FAQ
kept (one stale entry rewritten under an unchanged anchor); the eight
phase runbooks and GUIDE.md reduced to 5-line forwarding stubs;
`scripts/check-doc-links.py` wired into the hygiene job so a broken
relative link or anchor fails CI; code comments repointed in #22.
`egress.md` / `inbound.md` reserved for P7a/P7b by path, not linked
(a link to a missing file would fail the new gate).

Coordinator verification (independent, 2026-09-01): link checker green on
main (24 files); every `make` target named in the capability docs and FAQ
exists in the Makefile (41 named, all resolve — the only misses were prose
words); banned-phrase grep over the changed docs clean; honesty markers
survived (68 across the capability docs), and ten load-bearing caveats
spot-checked verbatim — the agent-is-never-the-one-denied finding, the
five schema-valid-only presets, the Copilot undocumented-surface note,
at-least-one-bound on grants, the AKS one-off-then-torn-down scope, the
`chat:write.public` warning, the relaying-side model failure, the
`imagePullPolicy: Never` rationale, and the NetworkPolicy gap (named in
four docs). Post-merge main CI green.

Rulings — all ACCEPTED: the use-it / govern-it split (models vs spend,
tools vs tool-governance) is the right seam because the ungoverned path
is a real shipped choice; lowercase single-word filenames matching the
P7a/P7b convention; stubs kept because PR descriptions, this board and
code comments link the old names; the editorial rule (commands, on-screen
output, caveats and verification STATUS stay; alternatives-considered,
provenance and board numbers move out — this board holds them) is exactly
the split the prompt asked for and it is written down where readers can
see it; the "no command was executed live" note is honest and acceptable
for a pure restructure — the carried outputs are labelled as recorded
runs with dates. Deviation 1 (branched before #20) had no effect: the
lane touched nothing #20 touched.

Carried forward: decide after the parallel set merges whether the stubs
can go (nothing in-tree needs them once #22 repointed the comments; PR
descriptions and this board are the remaining referrers). Line count
4,351 → 4,243 confirms the "navigation, not duplication" diagnosis.

### P5b — cluster portability + a real AKS run (PR #19, merged 2026-09-01)

Delivered on main: the portability refactor (no new abstraction layer —
`KUBE_CTX` became overridable, Kustomize evaluated and REJECTED because
its `images:` transformer takes only static values, which would have
forced committing the registry name this lane must not commit);
`scripts/aks-up.sh` / `aks-down.sh` (parameterised, tagged, AcrPull
verified rather than blindly re-attached); `scripts/kube-guard.sh` +
its test suite; `scripts/check-no-azure-ids.sh` run by CI;
`scripts/plane-deploy.sh` (renders the environment-dependent pull
policy by parsing, not grepping); `docs/P5B-RUNBOOK.md`. AKS run
completed and the cluster torn down.

Coordinator verification (independent, 2026-09-01):
- **Guardrail 1 — no Azure identifiers.** My own scan of the tracked
  tree AND the lane's commit history for GUIDs, registry hostnames
  (`<name>.azurecr.io`) and cluster FQDNs: every hit is a variable
  expansion (`$(ACR_NAME)`,
  `$ACR`) or a comment inside the scanner explaining what it blocks. No
  subscription, tenant, RG, registry, or FQDN literal reached the public
  repo or its history.
- **Teardown actually happened.** `az group list` shows no `kaimahi`
  resource group; the AKS clusters that do exist in that subscription
  belong to unrelated projects. No lingering spend.
- **Context guard genuinely fails closed.** Beyond the worker's own
  passing test suite, I ran the guard against a REAL remote AKS context
  on this machine: it printed the banner (context, API host, namespaces,
  "REMOTE / non-kind") and REFUSED with exit 1, naming the exact
  `KAIMAHI_CONFIRM` needed. Against local kind it passed silently. The
  two-independent-checks design (context name AND loopback API server)
  is right — a context name proves nothing.
- **Kind unregressed** (the main risk flagged at GO): `make status`
  healthy and a governed chat completed on the live kind cluster from
  merged main.

CI FINDING — main went red once, then green on re-run; ruled a real but
intermittent pre-existing race, NOT a P5b regression. The old `chat`
recipe's `port-forward & sleep 3` had been incidentally serving as the
agent's readiness wait. P5b replaced it with a correct port-forward
readiness poll, removing ~2.5s of padding and EXPOSING a race that was
always there: kagent's agent pod has `readinessProbe
initialDelaySeconds: 15`, and during a preset-switch rollout the old pod
has left the Service before the new one is programmed into kube-proxy.
RULING: exposing the race rather than restoring the padding was the
correct call and the worker said so explicitly ("restoring the sleep
would have made CI green and left the repo relying on padding — which is
what hid this in the first place"). The MITIGATION is incomplete: the
bounded retry keys on `connection refused`, but the post-merge failure
was `Post "http://hello-tools.kagent:8080": EOF` — the same race one
moment later (connection accepted, then torn down). **Follow-up:** widen
the retry predicate to cover EOF and connection-reset, keeping it keyed
narrowly enough that it cannot mask a real outage.

Other rulings — all ACCEPTED: `desired` model-config step and
`govern`-before-agents ordering (both no-ops on kind); `ollama`/`model`
refuse on `TARGET=aks` rather than half-deploying; the coordinator's
storage-class hypothesis CORRECTED (AKS 1.35.7's default StorageClass is
literally named `default`, not `managed-csi` — the PVC works either way,
and the runbook records what happened rather than the assumption);
Copilot-secret-before-plane ordering (an *optional* secret volume comes
up empty and kubelet projects it minutes later, which looks like a
broken deploy rather than a race); `up` no longer guards a cluster it is
about to create.

Carried forward:

- The retry-predicate widening above.
- **The foot-gun fired in-lane, exactly as predicted.** The `tool-*-probe`
  scripts bypass the Makefile guard (CI and humans run them directly), so
  they inherited `kubectl config current-context` — which
  `az aks get-credentials` silently rewrites. A kind denial probe was
  aimed at the new AKS cluster and only failed because the Secret happened
  not to exist there. Now guarded, resolving the effective context with
  `config view --minify` (which honours a `--context` inside `$KUBECTL`;
  `config current-context` does not, and would guard a different cluster
  than the one acted on).
- Concurrent kind+AKS verification runs collide on fixed local ports
  (`plane-admin.sh` 19091, probes 18081).
- A gate that reports noise stops being read: the identifier scanner
  went from 132 findings to precise once it scanned tracked files only.

### P5a — governed Slack outbound (PR #18, merged 2026-09-01)

Delivered on main: `k8s/slack-mcp.yaml` (in-cluster Slack MCP server,
pinned, `--no-cache`), `k8s/kaimahi-slack.yaml` (gateway upstream +
Kaimahi-owned RemoteMCPServer), `k8s/slack-agent.yaml`,
`scripts/slack-secret.sh` (stdin-only, xoxb- prefix validated),
gateway-injected per-upstream credentials in `plane/`, `docs/P5A-RUNBOOK.md`,
keyless CI assertions. Only route to Slack is through the gateway — no
ungoverned contrast path ships.

Coordinator verification (independent, 2026-09-01): custody clean — tree
scan finds no token (the three `xoxb` hits are a rejection test fixture
and the capture script's own prompt/validation); agent-namespace Secrets
hold ONLY `kmh_` tokens, while the real `xoxb-` bot token lives
plane-side in the `kaimahi` namespace; config.Parse rejects inline
credentials and a header-without-file at load ("key material never
belongs in the committed table"). Post-merge main CI green (all three
jobs). The discovery finding reproduced independently: the agent SELECTS
`[conversations_history, conversations_add_message]` but discovery
projects only `conversations_history` (the live allowlist) — the post
tool is named in the agent's spec yet absent from its hands.

RULING on deviation 2 — the lane prompt's demo shape was WRONG, and the
correction is an improvement. W8 specified "an agent tries to post and is
DENIED". kagent computes an agent's toolset as `discovered ∩ toolNames`
and discovery flows through the gateway, so a non-allowlisted tool is
never projected and the agent never attempts it. The security property is
STRONGER than specified: the capability does not exist until approved, so
the model cannot be prompt-injected into attempting it, cannot hallucinate
its availability, and cannot leak that it exists. **Corrected demo
narrative for anyone presenting this:** approval is CONSTRUCTIVE — the
capability materialises on approval and evaporates on exhaustion; the
deny-and-file path is exercised at the gateway by any direct MCP client
(what CI asserts). The worker documented this rather than faking the
prompt's shape, which is the correct call.

Other rulings — all ACCEPTED: gateway-injected upstream credentials
(net-new plane code, user-ruled mid-lane: keep it and document that the
chosen server ignores it — it is the right plane mechanism for any future
keyed upstream, and it fails closed at 503 rather than forwarding bare);
`toolNames` is selection while the allowlist is authority (CI's assertion
correctly moved to the LIVE allowlist, not committed YAML); pre-forward
use consumption so a 503 burns a use and audits as `allowed 503` (follows
P4c's conservative-direction ruling); `--no-cache` (its caches would pull
a workspace directory into the pod); no ungoverned Slack path; NetworkPolicy
declined as out-of-scope with an honest accounting (three non-network
layers bound blast radius; promoted to a named candidate above).

Carried forward:

- **Board-level lesson — a verification tool can itself fail open.** The
  worker's own probe reported ADMITTED for any 503, but the gateway
  answers 503 from four pre-forward DENIAL paths, so a Postgres blip
  would have verified as success. Standing guidance already says a verify
  path accepts only a well-formed positive; this is the reminder that the
  rule binds probes and CI assertions, not just product code.
- **User action owed (workspace-side, not repo-side):** the Slack app
  carries `chat:write.public`, which lets the bot post to any public
  channel without being invited. Worker recommends removal; only the
  workspace owner can do it.
- Measurement beat documentation twice (upstream README and a web survey
  both wrong about API-key enforcement and streamable-HTTP support) —
  run the image, believe the run.

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

### P2 — hosted-LLM ModelConfig presets (PR #3, merged 2026-08-31)

Delivered on main (d1a584d): seven presets in `k8s/models/` (anthropic,
openai, openrouter, azure-foundry, openai-compatible, ollama,
github-copilot), `make use PRESET=x` switching (merge-patches the Agent;
`k8s/hello-world.yaml` never mutated), stdin-only key custody
(`make model-secret`), device-flow Copilot token custody
(`scripts/copilot-secret.sh` + `make copilot-secret`), `docs/P2-RUNBOOK.md`,
keyless CI extensions (server-side dry-run of presets + ollama switch e2e).

Coordinator verification (independent, 2026-08-31): main tree byte-identical
to the checks-green branch (tree 528da638); PR checks + post-merge main run
33442951163 green (hygiene + e2e); GitHub Models retirement verified
externally (changelog live, models.github.ai returns 410 unauthenticated);
`scripts/copilot-secret.sh` reviewed against the custody rules (pipefail,
umask 077, pipes/0600-only token bytes, non-empty checks before kubectl, no
redirect-following on keyed calls, dry-run|apply with no delete-then-create
gap); live cluster spot-checked (agent on ollama preset, chat
state=completed, github-copilot ModelConfig present from keyed run).

Deviations (worker-reported; carried forward):

- **GitHub Models retired 2026-07-30** → D7 unexecutable → D8 pivot to the
  Copilot subscription API via device flow (gh tokens 403 at the exchange).
  Live-verified end to end (gpt-5-mini, state=completed, usage metered by
  the endpoint). Token expires; re-run `make copilot-secret` to rotate —
  auto-refresh deliberately deferred to P4 governance.
- **Fail-open Secret bug caught pre-merge**: make-recipe pipeline (dash, no
  pipefail) stored an empty Secret on a failed exchange; rewritten as a
  fail-closed script. Now standing security guidance (above).
- **README D6 wording adjusted** for the retirement (flagged by the worker;
  coordinator finds the new wording consistent with D6+D8).
- Only ollama + github-copilot are live-verified; the other five presets are
  schema-valid (server-side dry-run in CI) and marked not-live-verified in
  the runbook.
- `k8s/models/ollama.yaml` duplicates hello-world-model's substance so
  switching is uniform; the P1 artifact stays self-contained.
- Anthropic preset defaults to `claude-opus-5`; `model:` is a one-line edit
  per preset.

### P3 — MCP connectors/tools (PR #4, merged 2026-08-31)

Delivered on main (99edd8a): kagent's bundled tool server enabled and
locked down via `k8s/kagent-values.yaml` (read-only RBAC, Secrets
explicitly excluded), `k8s/tools-agent.yaml` (hello-tools Agent wired via
spec.declarative.tools), `make tools-agent` / `make chat AGENT=...`,
`docs/P3-RUNBOOK.md`, keyless CI e2e extended with a fail-closed
tool-invocation assertion (A2A function_call parts).

Coordinator verification (independent, 2026-08-31): branch-vs-main diff is
the two D10 board lines only — P3 payload identical; PR checks + post-merge
main runs green (e2e 6m10s incl. tool step); live cluster check ran a fresh
tool-requiring task → real function_call, state=completed; hello-tools
Ready, chart-managed RemoteMCPServer Accepted.

Coordinator ruling on the flagged deviation: tool server via helm values
(not a standalone committed CRD YAML) is ACCEPTED — the chart ships the
Deployment + RemoteMCPServer; committing a duplicate would shadow the
chart-managed resource and violate the prime directive. The lockdown block
in kagent-values.yaml is the committed artifact.

Deviations (worker-reported; carried forward):

- ToolServer v1alpha1 is legacy at 0.9.12; MCPServer/RemoteMCPServer is the
  supported path (runbook records it).
- RemoteMCPServer's first reconcile can race the tool-server pod
  (Accepted=False, self-heals ~1 min); glue waits on Accepted before
  applying the agent.
- New small-model failure mode: correct tool call + correct response but
  WRONG summary (claimed emptiness). P1's delta covered malformed calls;
  this is the relaying side. Mitigated via system-message wording (10/10
  after); swap-a-model testers must re-measure both failure modes.
- hello-tools requests shrunk (50m/320Mi) for the 2-CPU CI runner — node
  was at ~95% requests before the shrink; P4 must budget accordingly.
- `make up` is cumulative (includes the tools agent), P1/P2 e2e steps
  unchanged.

### Rename lane — tomte → kaimahi in-repo (PR #5, merged 2026-08-31)

Delivered on main (01f5c3c): rename across README, runbooks, Makefile,
scripts, k8s (incl. agent systemMessages — the authorized one-time
hello-world.yaml mutation), CI. Delegated choices: `KIND_CLUSTER=kaimahi-p1`
(old clusters keep working via override; migration note in P1 runbook) and
`KAIMAHI_COPILOT_TOKEN_FILE` / `~/.config/kaimahi/` (mv note in P2
runbook). Worker live-verified on a fresh kaimahi-p1 cluster including the
tool round-trip.

Coordinator verification (independent, 2026-08-31): tracked-tree grep audit
— only surviving "tomte" hits outside this board are the two justified
migration notes; delegated choices confirmed in Makefile/script; post-merge
main CI green (full P1+P2+P3 e2e, 6m13s). Board's own present-tense
references renamed by the coordinator in this commit (historical
quotes/delta sheets stay verbatim). No deviations reported; scope held.

### P4a — metering/enforcing LLM proxy (PR #12, merged 2026-09-01)

Delivered on main: `plane/` Go module (P4b/P4c extend it), `k8s/plane/`
(namespace kaimahi, proxy + Postgres 16 + PVC, operator-configured
upstream table), governed presets `k8s/models/governed-{ollama,copilot}`,
make targets (plane/govern/budget/ledger), `scripts/plane-admin.sh`,
`docs/P4A-RUNBOOK.md`, CI `go-plane` job + governed e2e assertions in the
existing cluster job. Port evaluation per package in the PR (redact/db
PORT, meter/pricing/proxy ADAPT, vault/permit/SDKs/store-shell SKIP with
reasons, store REWRITE around the spend-ledger pattern).

Coordinator verification (independent, 2026-09-01): P4a payload on main
byte-identical to the branch (remaining tree delta = PRs #10/#11 docs);
main CI green (go-plane + e2e incl. governed assertions); live re-run by
the coordinator on kaimahi-p1 — governed chat completed and ledgered
(367/25 tokens, source=free, 200), token-cap exhaustion failed CLOSED
("monthly token budget reached", three denied 429 rows themselves
ledgered), custody proven (agent-side Secret holds a `kmh_` opaque token;
Postgres `credential.token_hash` is a 32-byte sha256, no plaintext; proxy
Service exposes 8080 only — admin 9091 reachable solely via port-forward
+ bearer token).

Coordinator rulings on flagged deviations: vault SKIP accepted (K8s-Secret
custody + hash-only DB replaces envelope encryption; no requirement behind
a master key). Token caps alongside cents caps accepted (only honest lever
on the $0-classified ollama tier; no invented prices — Copilot governed by
token caps, and under a cents budget an unpriced metered model is denied
pre-forward). Soft-stop budget semantics (small in-flight overshoot)
accepted for P4a, revisit with P4c approvals. `imagePullPolicy: Never`
decline accepted (a side-loaded local tag must never fall back to pulling
a squattable public name).

Deviations carried forward:

- Ledger `cost_source ∈ {free,priced,unpriced,denied}` — every $0 row
  carries its explanation; denials are ledgered (zero usage, real status).
- Fail-closed ledger degradation: a failed ledger write trips the data
  plane to 503 — spend that can't be recorded must not happen.
- Streaming usage: proxy injects `stream_options.include_usage` and scans
  the SSE tail; upstreams reporting no usage record zero tokens + a
  warning (never invented). Known limitation in the runbook.
- CI node is effectively full (~1935m/2000m requests with the plane
  deployed; a CI-only Agent-CRD patch shrinks hello-world's runtime
  requests). P4b MUST budget CPU requests before adding anything.
- Pre-existing hygiene-CI bug (deviation 11): the "No secrets in tree"
  step's `! grep` inverts exit codes so a grep ERROR (exit 2) passes the
  gate — fail-open. Fix assigned to the coordinator's reconciliation PR.

### P4b — enforcing MCP gateway (PR #15, merged 2026-09-01)

Delivered on main (97c2b5f): `plane/internal/gateway` — a second listener
in the existing proxy process (zero added CPU requests) relaying MCP
streamable-HTTP and enforcing fail-closed; `k8s/kaimahi-tools.yaml`
(Kaimahi-owned RemoteMCPServer at the gateway; chart-managed server
untouched per the P3 ruling); separate `hello-tools` credential +
`kaimahi-tools-token` Secret carried via headersFrom; `tool_audit` table;
make targets govern-tools/ungovern-tools/tool-allow/tool-allowlist/
tool-audit; `scripts/tool-denial-probe.sh`; `docs/P4B-RUNBOOK.md`; CI
gateway assertions (governed probe call, allowed-200 row, denial +
denied-403 row, custody + projection checks).

Coordinator verification (independent, pre-merge at 06873d2, payload
identical on main): projection (upstream 8 tools → credential sees 1);
governed round-trip with a coordinator-minted probe (function_call +
probe in reply + allowed-200 audit row); non-allowlisted call denied
(JSON-RPC -32001, denied-403 row) — coordinator's own timestamps; custody
(Secret matches ^kmh_[0-9a-f]{64}$, zero kmh_ occurrences in proxy logs,
hash-only DB); code read confirmed denied-methods-never-relayed and the
audit-breaker (healing request itself denied) are test-asserted.
Post-merge main CI green (go-plane + hygiene + full e2e).

Coordinator rulings on the nine deviations — all ACCEPTED: same-pod
gateway (CPU ceiling); MCP lifecycle additions (notifications/initialized
relayed, ping answered locally, batches rejected, GET 405, DELETE
relayed); tool_audit as its own table (ledger cost semantics don't
describe actions; fail-closed machinery shared); per-seam credential;
govern-tools ordering; image tag moves with the phase (imagePullPolicy
Never rationale); SSE→JSON re-emit on tools/list with unparseable
listings failed closed; the W6 shared-cluster disruption (rule already on
the board); known limitations recorded (NetworkPolicy egress unbuilt,
projection refresh on reconcile, allowlist per-credential not
per-upstream). Blueprint adaptations (permits→static allowlist until
P4c; pinned snapshots→live projection; SSRF dialer deferred while the
upstream table is single-entry in-cluster) are consistent with the lane
prompt.

Carried forward for P4c:

- The static allowlist is the permit model's placeholder — P4c's
  approvals should compile down to it (and may pin tool snapshots, per
  the blueprint, once approvals can pin).
- Relay-then-audit ordering is the accepted P4a ledger contract applied
  to actions; revisit only if P4c's approval semantics demand
  pre-commit audit.
- NetworkPolicy egress and internet-facing tool upstreams (with the
  blueprint's hardened dialer/SSRF set) remain unbuilt and documented.

### P4c — approvals and time-boxed permits (PR #17, merged 2026-09-01) — ARC COMPLETE

Delivered on main (dd08f00): deny-and-pend approvals in plane/ (denied
tool calls and budget denials auto-file deduped pending requests);
bounded grants (TTL and/or uses, at least one bound REQUIRED — unbounded
approve refused) compiling into the P4b allowlist and P4a budget checks,
liveness evaluated at decision time by the same SQL predicate the CLI
shows; approval audit trail (requested/approved/denied with bounds);
make approvals/approve/deny/request/grants/approval-audit +
plane-admin.sh subcommands; scripts/tool-call-probe.sh (positive half of
the probe pair); docs/P4C-RUNBOOK.md; README/status updated to
"governance thesis, first full pass"; CI asserts both full cycles
keyless. Also in: the board-backlog make-up governance-preservation
guard (covers modelConfig AND the hello-tools gateway wiring — the W6
disruption's actual footgun) and the same-tag redeploy trap fix.

Coordinator verification (independent, pre-merge at 630fcea): both
cycles reproduced with coordinator-minted requests and timestamps —
tool: 14:31:52 denied+auto-filed → 14:32:05 USES=1 grant → 14:32:08
allowed-200 audit row CITING the grant id → 14:32:09 exhausted, denied
again, fresh request filed; budget: 14:32:29 cap denial auto-filed →
bounded grant (uses=1 amount=5000) → chat completed → next chat denied
(429s ledgered) → new request filed. Unbounded approve refused. Denials
remain enforcement-audited throughout (approval state never suppresses
ledger/tool_audit). Post-merge main is the verified payload.

Coordinator rulings on the eight deviations — all ACCEPTED: transactional
decision audit vs logged-only auto-filing (correct asymmetry — the
enforcement trail still records every denial; 503ing over a convenience
record would be worse); pre-forward tool-use consumption (conservative);
projection includes live grants while agent toolNames stays static
(discovery-lag honest); append-only grant history; admin-bearer as the
approver identity (per-approver identity deferred with approval routing);
oldest-first summing budget grants; the widened backlog-fix scope; tag
moves with phase.

Known limitations carried forward (documented): per-approver identity and
approval routing (the parked connectors candidate is the natural
delivery); NetworkPolicy egress; internet-facing upstreams + SSRF set;
live kaimahi-p1 DB carries manual ALTERs matching migration 00003 (fresh
clusters get them from the migration; rebuild the demo cluster if drift
ever matters).
