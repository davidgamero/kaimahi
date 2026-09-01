# P4b runbook — the enforcing MCP gateway

P4a governed what an agent *spends*; P4b governs what it *does*. The
gateway mounts at kagent's tool-server seam — the RemoteMCPServer URL —
and every MCP call a governed agent makes is authenticated, scope-checked,
allowlist-enforced, and audited before it reaches a tool server. kagent
still runs the tools; Kaimahi ships **no MCP runtime** — the gateway
relays the protocol and enforces.

> **⚠️ P4b governs tool calls through the gateway.** Approvals /
> human-in-the-loop are still absent (P4c) — the allowlist is static, not
> a consent flow. And the gateway is an *application-layer* egress rule:
> a pod that ignores its RemoteMCPServer wiring can still open arbitrary
> connections until cluster-level NetworkPolicy lands (a known
> limitation, deliberately not built in this slice).

## Architecture

```text
namespace kagent                             namespace kaimahi
┌──────────────────────┐  Authorization:   ┌─────────────────────┐
│ hello-tools agent    │  kmh_… (Secret-   │ kaimahi-proxy pod   │
│   tools[] ──▶ kaimahi-│  resolved via    │  :8080 LLM proxy    │   committed
│   tools               │  headersFrom)    │  :8081 MCP GATEWAY ─┼──▶ tool_upstreams
│ RemoteMCPServer       │ ───────────────▶ │  authn → scope →    │   table: ONLY
│   url = gateway       │                  │  allowlist → relay  │   kagent-tools
└──────────────────────┘                  │  → audit            │        │
                                           └─────────┬───────────┘        ▼
kagent controller discovery                          │            http://kagent-tools
(initialize + tools/list) rides            ┌─────────▼──────────┐  .kagent:8084/mcp
the same path — discoveredTools            │ Postgres 16        │  (chart-managed,
IS the allowlist projection                │ tool_allowlist,    │   read-only lockdown
                                           │ tool_audit         │   from P3)
                                           └────────────────────┘
```

- **Placement**: the gateway is a second data listener (`:8081`) in the
  existing `kaimahi-proxy` process — it reuses the plane's Postgres pool,
  credential model, log redactor, and fail-closed machinery, and adds
  **zero** CPU requests (the CI node runs ~full). A dedicated Service,
  `kaimahi-mcp-gateway`, gives the tool seam its own address; the admin
  port stays off every Service.
- **The seam**: [`k8s/kaimahi-tools.yaml`](../k8s/kaimahi-tools.yaml), a
  Kaimahi-owned RemoteMCPServer whose URL is the gateway. The
  chart-managed `kagent-tool-server` is neither shadowed nor mutated (P3
  ruling) — this is a second, governed front door.
- **Upstreams**: the `tool_upstreams` table in
  [`k8s/plane/upstreams.yaml`](../k8s/plane/upstreams.yaml) — the ONLY
  places the gateway will relay to. One entry in this slice: the
  in-cluster kagent tool server.

## Credential custody

`make govern-tools` mints a separate `hello-tools` credential; the
agent-side Secret `kagent/kaimahi-tools-token` holds only the `kmh_…`
opaque token (the plane stores its sha256), and the RemoteMCPServer sends
it via `headersFrom`. The gateway strips it (and every credential-slot
header) before relaying — the token never reaches a tool server. This
lane's upstream is in-cluster and unauthenticated; a future keyed tool
server gets its real credential injected from proxy-side custody, exactly
like the P4a LLM upstreams.

## From zero

```sh
make up             # P1–P3: cluster, ollama, kagent, agents
make plane          # P4a+P4b: proxy + gateway + Postgres
make govern-tools   # credential, allowlist, gateway wiring for hello-tools
make chat AGENT=hello-tools TASK='What pods run in the ollama namespace?'
make tool-audit     # the call you just made, in the audit trail
```

`make ungovern-tools` restores the P3 wiring (direct, ungoverned).
Re-run `make plane` after editing `upstreams.yaml` — the config is read
at boot (the ConfigMap mounts via subPath, which never live-updates).

## Enforcement — all fail-closed

- **Egress**: only `tool_upstreams` entries are reachable; any other
  upstream name answers 403 before any network contact.
- **Protocol scope**: tools only. `initialize` and
  `notifications/initialized` (the mandatory lifecycle handshake) relay;
  `tools/list` and `tools/call` relay under governance; `ping` is
  answered locally without upstream contact; **every other method is
  denied, not relayed** (JSON-RPC error, audited). JSON-RPC batches are
  rejected outright — a batch could smuggle a denied method.
- **Allowlist**: enforced on `tools/call` and **projected** on
  `tools/list` — kagent's controller discovers through the gateway, so
  `status.discoveredTools` on `kaimahi-tools` shows exactly what the
  credential may call; the agent never sees the rest. **Empty or missing
  allowlist = nothing callable** — the one governed exception is a live
  P4c time-boxed grant ([P4C-RUNBOOK.md](P4C-RUNBOOK.md)), which admits
  calls and joins the projection while it lasts. Allowlist edits enforce
  immediately on
  calls; the projection an agent *sees* refreshes on kagent's next
  RemoteMCPServer reconcile.
- **Audit**: every `tools/call` outcome and every attributable denial is
  appended to `tool_audit` (credential, upstream, method, tool,
  decision, status) — pre-auth 401/503 refusals have no credential to
  attribute, like P4a. Allowed rows are written after the response they
  describe (P4a's ledger contract); **a failed audit write trips the
  gateway to 503 for all subsequent traffic** until a write succeeds —
  unrecordable actions must not continue happening.
- **Auth**: same as P4a — unknown token 401, credential store unreadable
  503, no upstream contact either way.

```sh
make tool-allow TOOLS=k8s_get_resources,k8s_get_events   # widen
make tool-allow TOOLS=-                                  # nothing callable
make tool-allowlist                                      # show
bash scripts/tool-denial-probe.sh k8s_describe_resource  # watch a denial
```

The denial probe calls a non-allowlisted tool with the governed token and
requires the JSON-RPC `-32001` "not permitted" error; the attempt lands
in `make tool-audit` as a `denied 403` row. Note: `make govern-tools
TOOLS=...` keeps the agent's `toolNames` aligned with the allowlist it
sets; `make tool-allow` alone changes only the gateway policy — re-run
`govern-tools` (or widen `toolNames` yourself) if the agent should *use*
newly allowed tools. The allowlist is the governance boundary either way.

## Verification status

Asserted keylessly in CI on every PR: the governed tool call through the
gateway with the P3 probe-ConfigMap proof, the `allowed 200` audit row,
a non-allowlisted call denied `-32001` and audited `denied 403`, and
custody (agent-side Secret matches `^kmh_[0-9a-f]{64}$`, Secret-referencing
`headersFrom`, discovery projecting exactly the allowlist). Additionally
live-verified (and unit-tested): an empty allowlist denying even the
default tool, and the projection hiding 7 of the 8 tools the upstream
offers.

## Governed vs still ungoverned

| Surface | Status |
|---------|--------|
| LLM calls via `governed-*` presets | **Governed** (P4a): authn, budgets, ledger, custody |
| LLM calls via P2 presets (`openai`, `anthropic`, …) | Ungoverned (by choice — switch presets to govern) |
| Tool calls via `kaimahi-tools` (after `make govern-tools`) | **Governed** (P4b): authn, upstream table, tools-only scope, allowlist, audit |
| Tool calls via `kagent-tool-server` directly (P3 wiring) | Ungoverned (by choice — `make govern-tools` to govern) |
| Approvals / permits / human-in-the-loop | **Built in P4c** — denials file approval requests; time-boxed grants admit past the static allowlist ([P4C-RUNBOOK.md](P4C-RUNBOOK.md)) |
| Pod-level network egress | **Not enforced**: the gateway governs the MCP seam only; cluster NetworkPolicy is a known limitation, not yet built |

## Operational notes

- The gateway shares the proxy's lifecycle: `make plane` rebuilds and
  rolls both (image tag `kaimahi-proxy:p4b`; the tag moves with the phase
  so a stale side-loaded image can never satisfy a newer manifest under
  `imagePullPolicy: Never`).
- `make govern-tools` is idempotent and ordered so discovery never sees
  an empty projection by accident: credential → allowlist → the
  RemoteMCPServer (waits Accepted) → agent patch (waits Ready).
- If the RemoteMCPServer sits at `Accepted=False` just after `make
  plane`, the first reconcile raced the proxy rollout; it self-heals
  within a minute (same behavior P3 recorded for the chart's own server).
- The allowlist is per-credential, not per-(credential, upstream): with
  one committed tool upstream that distinction is empty, but adding a
  second upstream widens every existing allowlist across both — scope
  the allowlist per upstream before growing the table (P4c territory).
- The audit trail is demo-durable like the ledger: it survives pod
  restarts via the Postgres PVC, and `make down` destroys it.
