# P4a runbook — the metering/enforcing LLM proxy

P1–P3 built the agent; P4 builds what Kaimahi actually sells: governance.
P4a is the first slice — a metering and enforcing LLM proxy mounted at
kagent's ModelConfig `baseUrl` seam (D11). Every model call an agent makes
through a governed preset is authenticated and budget-checked before it
is forwarded, and ledgered (denials immediately; forwarded calls once the
response reveals their usage) — and the real upstream credential lives
only with the proxy. **Keys never reach the agent.**

> **⚠️ P4a governs LLM traffic only.** MCP/tool calls are still
> ungoverned (the enforcing MCP gateway is P4b), and there is no approval
> workflow yet (P4c). The ungoverned P2 presets (`make use PRESET=openai`
> etc.) also still exist — governance applies only when the agent is on a
> `governed-*` preset.

## Architecture

```text
Agent pod (kagent)                         namespace kaimahi
┌─────────────────────┐   opaque token   ┌──────────────────┐    real creds
│ governed-ollama      │ ───────────────▶ │  kaimahi-proxy   │ ──────────────▶ upstream
│ ModelConfig          │  (kmh_…, issued  │  authn → route → │  (ollama: none; │
│ baseUrl = proxy      │   by the plane)  │  budget → fwd →  │   copilot: Secret
│ apiKeySecret = token │                  │  ledger          │   mounted to proxy)
└─────────────────────┘                  └────────┬─────────┘
                                                  │
                                         ┌────────▼─────────┐
                                         │ Postgres 16      │  credentials (hashes),
                                         │ (durable store)  │  budgets, spend ledger
                                         └──────────────────┘
```

- **The plane** (`k8s/plane/`): namespace `kaimahi`, the proxy
  Deployment/Service, Postgres 16 with a PVC. Migrations are embedded in
  the proxy binary and run idempotently at startup — a rollout is its own
  migration step.
- **The mount** (`k8s/models/governed-*.yaml`): ModelConfigs whose
  `openAI.baseUrl` points at the proxy. kagent needs no changes — this is
  the seam working as designed.
- **Upstreams** ([`k8s/plane/upstreams.yaml`](../k8s/plane/upstreams.yaml)):
  exactly the two live-verified paths — in-cluster ollama (the free demo
  tier) and the Copilot subscription endpoint (D8). One base URL and **one
  allowed (method, path)** per upstream is the whole blast radius; any
  other request is denied before any upstream contact.
- **Admin plane**: a second port (9091) that the Service deliberately does
  not expose — `make govern|budget|ledger` reach it via
  `kubectl port-forward` + a bearer token from the `kaimahi-admin` Secret,
  so cluster credentials gate every admin operation.

Provenance: ported/adapted from the archived
[tomte-old](https://github.com/gambtho/tomte-old) Go stack per the prime
directive — the per-package port/adapt/rewrite/skip record is in the P4a
PR description.

## Credential custody (the mission sentence)

- The agent-side Secret (`kagent/kaimahi-governed-token`) holds a
  **Kaimahi-issued opaque token** (`kmh_…`), minted by `make govern` and
  shown exactly once; the plane stores only its sha256.
- The **real** Copilot token lives in `kaimahi/kaimahi-copilot-token`,
  mounted to the proxy pod and read per request (rotation via
  `make plane-copilot-secret`, no restart). Ollama has no credential at
  all and is forwarded bare — nothing is injected by contract, not by
  accident.
- The proxy strips the opaque token (and any other credential-slot
  header) before injecting the real credential upstream; keyed calls
  never follow redirects; logs pass through a redactor.

## From zero

```sh
make up          # P1–P3: cluster, ollama, kagent, agents
make plane       # build the proxy image, load into kind, deploy plane
make govern      # issue the credential, switch hello-world through the proxy
make chat        # governed: authenticated, metered, ledgered
make ledger      # see the row the chat just wrote
```

`make govern` leaves `hello-world` on the `governed-ollama` preset and
also applies `governed-copilot` (switch with
`make use PRESET=governed-copilot` once `make plane-copilot-secret` has
given the proxy the real token).

## Budgets — fail closed

Caps are monthly (calendar month, UTC), per credential, set via CLI:

```sh
make budget CAP_TOKENS=100          # token cap (the free tier's only lever)
make budget CAP_CENTS=500           # cents cap
make budget                         # no caps
```

Demo of exhaustion failing closed:

```sh
make chat                           # succeeds, ledgered
make budget CAP_TOKENS=1            # below what any chat costs
make chat                           # task fails: "monthly token budget reached"
make ledger                         # the denial is ledgered too (429, zero usage)
```

Enforcement properties (all unit-tested and live-verified):

- An exhausted budget denies with **429** and a clear error before any
  upstream contact; the agent surfaces it as a failed task.
- If the ledger store is unreadable, budgeted credentials are denied
  (**403 "metering unavailable"**) — no spend visibility, no spend. A
  credential with no caps skips the budget read, but a failed ledger
  *write* trips the whole data plane closed (**503 "spend ledger
  unavailable"**) until a write succeeds again — spend that cannot be
  recorded must not happen.
- Every attributable outcome is ledgered — success, upstream failure,
  and denial (unauthenticated requests have no credential to attribute)
  — and billed usage is recorded even when the surrounding request fails
  (spend before failures are honored).
- Forwarded traffic meters through the OpenAI `usage` object (streamed
  requests get `stream_options.include_usage` injected); denials are
  fixed zero-usage rows. If an upstream response carries no usage at
  all, the row records zero tokens and the proxy logs a warning — token
  counts are never invented, so keep upstreams on OpenAI-compatible
  surfaces that report usage.

## Pricing — nothing is invented

`cost_source` in the ledger says why every cost is what it is:

- **free** — the upstream is explicitly classified `free` in
  `upstreams.yaml` (in-cluster ollama). $0 is a classification, never an
  inference.
- **priced** — a real price row (`prices` in `upstreams.yaml`, cents per
  1M tokens) was applied.
- **unpriced** — a metered upstream with no price row for that model:
  tokens are still counted honestly, cost stays 0. Under a **cents**
  budget an unpriced model is **denied** (the priced-pair gate ported
  from tomte-old) — use a token budget for Copilot unless you configure
  a price. No Copilot per-token price is bundled: subscription usage has
  no public per-token price and Kaimahi never invents one.
- **denied** — the request never went upstream.

## Verification status

- **governed-ollama**: live-verified end to end (chat → ledger row →
  budget denial → restore), and exercised keylessly in CI on every PR.
- **governed-copilot**: applied and schema-valid; the custody fail-closed
  path is live-verified (no proxy-side token ⇒ 503 "upstream credential
  unavailable" — the request never leaves the cluster). A full governed
  Copilot chat needs an interactive device login
  (`make plane-copilot-secret`), so per the P1 delta rule it is marked
  **not live-verified** here; run it locally to close the loop.

## Governed vs still ungoverned

| Surface | Status |
|---------|--------|
| LLM calls via `governed-*` presets | **Governed**: authn, budgets, ledger, custody |
| LLM calls via P2 presets (`openai`, `anthropic`, …) | Ungoverned (by choice — switch presets to govern) |
| MCP/tool calls via the `kaimahi-tools` RemoteMCPServer | **Governed by P4b** after `make govern-tools` — see [P4B-RUNBOOK.md](P4B-RUNBOOK.md); the direct `kagent-tool-server` wiring stays ungoverned |
| Approvals / permits / blast-radius workflows | **Built in P4c** — budget denials file approval requests; bounded raises admit overage ([P4C-RUNBOOK.md](P4C-RUNBOOK.md)) |
| Egress other than the configured upstreams | Not reachable **through the plane** (denied); pod-level network egress is unenforced until cluster NetworkPolicy (post-P4b limitation) |

## Operational notes

- The proxy image is side-loaded into kind (`imagePullPolicy: Never`); on
  a real cluster push it to a registry and override the image.
- `scripts/plane-secrets.sh` generates the Postgres password and admin
  token idempotently (existing Secrets are kept — regenerating the pg
  password under a live database would lock the proxy out).
- The admin API answers 503 if its token file is unreadable, 401
  otherwise-unauthenticated; credential issuance shows the token exactly
  once. If the agent-side Secret is lost, delete the credential row (the
  `make govern` error message carries the exact command) and re-issue.
- Postgres data survives pod restarts via the PVC; `make down` (cluster
  delete) destroys it — the ledger is demo-durable, not backup-managed.
