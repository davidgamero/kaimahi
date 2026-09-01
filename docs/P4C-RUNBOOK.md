# P4c runbook — approvals and time-boxed permits

The governance plane's final slice, and the last arc phase. P4a governs
what an agent spends, P4b what it does — P4c adds the human to the loop:
**a denied action files a pending approval request, and a CLI approval
mints a time-boxed grant** that widens enforcement exactly as far as the
human said, for exactly as long.

The model is **deny-and-retry** (D13): no held-open calls, no approval
flow inside MCP itself. The agent's denial says a request was filed; the
operator decides; the agent (or the operator) simply tries again.

## The cycle

```text
  agent / probe                    plane                        operator
       │  tools/call k8s_get_events  │                              │
       │ ───────────────────────────▶│ denied (allowlist)           │
       │ ◀─── JSON-RPC -32001 ────── │ + approval request FILED     │
       │      "request filed"        │   (deduped while pending)    │
       │                             │                              │ make approvals
       │                             │ ◀──── make approve ID=… ──── │ TTL=60s USES=1
       │  tools/call k8s_get_events  │       (bounded grant)        │
       │ ───────────────────────────▶│ ALLOWED via grant            │
       │ ◀────── tool result ─────── │ (use consumed, audited)      │
       │  …bound expires/exhausts…   │                              │
       │  tools/call k8s_get_events  │                              │
       │ ───────────────────────────▶│ denied again — an expired    │
       │                             │ grant is simply not a grant  │
```

## Grants are bounded, or they are not grants

- **At least one bound is required** — expiry (`TTL=`) and/or use count
  (`USES=`). An unbounded grant is a config change (edit the allowlist
  or the budget), not an approval; the plane refuses to mint one. This
  is the ported permit discipline: "an entry allowing nothing is an
  error" became "a grant bounding nothing is an error".
- **Expiry and exhaustion are enforced at decision time, in SQL** — the
  gateway and meter never act on a cached grant, and no cleanup job is
  needed for correctness: an expired grant simply stops matching the
  liveness predicate.
- **Budget grants carry an `AMOUNT`** (tokens or cents, matching the
  exceeded cap): the effective cap is `cap + Σ(live grant amounts)`. A
  use is consumed only by a request that needed the grant — under-cap
  traffic never burns one. Tool-grant uses are consumed per admitted
  `tools/call` (before the forward, so an upstream failure burns the
  use — the conservative direction).

## Demo 1 — tool widening (read-only posture untouched)

```sh
make govern-tools                                   # P4b state
bash scripts/tool-denial-probe.sh k8s_get_events    # denied; request filed
make approvals                                      # copy the ID
make approve ID=<uuid> TTL=60s USES=1
bash scripts/tool-call-probe.sh k8s_get_events '{"namespace": "default"}'   # succeeds
bash scripts/tool-denial-probe.sh k8s_get_events    # denied again (use consumed)
make tool-audit                                     # allowed row says: granted <grant-id>
```

`tool-call-probe.sh` does the full MCP handshake (initialize →
initialized → tools/call) through the gateway with the `hello-tools`
credential. The `tools/list` projection includes live-granted tools
(visible = callable right now); the agent's own `toolNames` selection is
untouched by grants, so a granted tool is exercised via the probe — the
static allowlist plus `make govern-tools TOOLS=…` remains the way to
widen what the *agent* wields. The P3 tool server stays read-only
throughout: grants widen *which* read tools a credential may call, never
the server's posture.

## Demo 2 — budget overage

```sh
make budget CAP_TOKENS=1        # below any real chat
make chat                       # fails: "monthly token budget reached;
                                #  approval request filed — run 'make approvals'"
make approvals                  # copy the ID (kind=budget, subject=tokens)
make approve ID=<uuid> TTL=5m USES=1 AMOUNT=100000
make chat                       # completes; make ledger shows the overage rows
make chat                       # denied again — the single use is consumed
make budget CAP_TOKENS=100      # restore whatever cap you actually want
```

## Queue mechanics

- **Auto-filed**: a gateway tool denial files `(credential, tool)`; a
  budget-cap denial files `(credential, tokens|cents)`. Deduped per
  `(credential, kind, subject)` while pending — retry loops cannot spam
  the queue. A filing failure never un-denies (denial is the safe
  state) and the P4b enforcement audit row still writes.
- **Explicit**: `make request KIND=tool SUBJECT=k8s_get_events` (tool
  requests default to the `hello-tools` credential, budget requests to
  `hello-world`; override with `CRED=`).
- **Decide**: `make approvals`, then `make approve ID=… [TTL=…] [USES=…]
  [AMOUNT=…]` or `make deny ID=…`. A decided request is immutable;
  fresh denials file fresh requests.
- **Inspect**: `make grants` (with liveness computed by the same
  predicate enforcement uses) and `make approval-audit`
  (requested/approved/denied, with bounds — the approvals' own
  append-only trail; approve/deny commit grant, status, and audit row
  in one transaction, so a decision that cannot be recorded does not
  happen).

## Governed vs still ungoverned — the arc's first full pass

| Surface | Status |
|---------|--------|
| LLM calls via `governed-*` presets | **Governed** (P4a): authn, budgets, ledger, custody |
| Tool calls via `kaimahi-tools` | **Governed** (P4b): upstream table, tools-only scope, allowlist, audit |
| Approvals / time-boxed permits | **Governed** (P4c): deny-and-pend, bounded grants, approval audit |
| LLM calls via ungoverned presets; tool calls via direct `kagent-tool-server` wiring | Ungoverned, by explicit choice — `make govern` / `make govern-tools` opt in |
| Pod-level network egress (NetworkPolicy) | **Not built** — the plane governs its seams; a pod that bypasses them can still egress |
| Internet-facing tool upstreams | **Not built** — the committed table is single-entry, in-cluster; going internet-facing needs the blueprint's hardened dialer/SSRF set |
| Approval routing (Slack/Discord/email, per-approver identity) | **Not built** — the queue is CLI-only; "who approved" is the admin bearer, not a person identity (P5 candidate) |

## Operational notes

- Approvals live in the same process, Postgres, and admin port as
  P4a/P4b (nothing new to deploy; image tag moves to
  `kaimahi-proxy:p4c`; re-run `make plane` to roll it — migrations are
  idempotent at startup).
- `make up` now **preserves** a governed agent across re-runs (it warns
  and restores a non-default modelConfig / gateway wiring instead of
  silently resetting them — the W6 footgun). `make use PRESET=ollama`
  and `make ungovern-tools` remain the explicit ways back.
- Grant rows are never deleted; exhausted/expired grants stay visible in
  `make grants` as `live=no` history. Demo-durable via the PVC, like the
  ledger.
