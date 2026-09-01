# Approvals and bounded grants

Budgets govern what an agent spends and the gateway governs what it
does. Approvals add the human to the loop: **a denied action files a
pending approval request, and a CLI approval mints a bounded grant**
that widens enforcement exactly as far as the human said, for exactly
as long. The bound can be an expiry, a use count, or both.

Assumes: the plane from [spend.md](spend.md), and for the tool demo the
gateway wiring from [tool-governance.md](tool-governance.md).

The model is **deny-and-retry**: no held-open calls, no approval flow
inside MCP itself. The agent's denial says a request was filed; the
operator decides; the agent (or the operator) tries again.

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
       │                             │ grant is not a grant         │
```

## Grants are bounded, or they are not grants

- **At least one bound is required**: expiry (`TTL=`) and/or use count
  (`USES=`). An unbounded grant is a config change (edit the allowlist
  or the budget), not an approval, and the plane refuses to mint one.
- **Expiry and exhaustion are enforced at decision time, in SQL.** The
  gateway and meter never act on a cached grant, and no cleanup job is
  needed for correctness: an expired grant stops matching the liveness
  predicate.
- **Budget grants carry an `AMOUNT`** (tokens or cents, matching the
  exceeded cap). The effective cap is `cap + Σ(live grant amounts)`. A
  use is consumed only by a request that needed the grant; under-cap
  traffic never burns one.
- **Tool-grant uses are consumed per admitted `tools/call`, before the
  forward.** An upstream failure therefore burns the use. That is the
  conservative direction, and it has a visible consequence on the Slack
  path ([slack.md](slack.md)).

## Demo 1: widening a tool allowlist

The tool server stays read-only throughout. Grants widen *which* read
tools a credential may call, never the server's posture.

```sh
make govern-tools                                   # gateway wiring in place
bash scripts/tool-denial-probe.sh k8s_get_events    # denied; request filed
make approvals                                      # copy the ID
make approve ID=<uuid> TTL=60s USES=1
bash scripts/tool-call-probe.sh k8s_get_events '{"namespace": "default"}'   # succeeds
bash scripts/tool-denial-probe.sh k8s_get_events    # denied again (use consumed)
make tool-audit                                     # allowed row says: granted <grant-id>
```

`tool-call-probe.sh` does the full MCP handshake (initialize,
initialized, tools/call) through the gateway with the `hello-tools`
credential. The `tools/list` projection includes live-granted tools, so
visible means callable right now. The agent's own `toolNames` selection
is untouched by grants, which is why a granted tool is exercised via the
probe here: the static allowlist plus `make govern-tools TOOLS=…`
remains the way to widen what the *agent* wields.

## Demo 2: budget overage

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
  `(credential, kind, subject)` while pending, so retry loops cannot
  spam the queue. A filing failure never un-denies (denial is the safe
  state), and the enforcement audit row still writes.
- **Explicit**: `make request KIND=tool SUBJECT=k8s_get_events`. Tool
  requests default to the `hello-tools` credential, budget requests to
  `hello-world`; override with `CRED=`.
- **Decide**: `make approvals`, then `make approve ID=… [TTL=…] [USES=…]
  [AMOUNT=…]` or `make deny ID=…`. A decided request is immutable; fresh
  denials file fresh requests.
- **Inspect**: `make grants` (liveness computed by the same predicate
  enforcement uses) and `make approval-audit` (requested / approved /
  denied, with bounds: the approvals' own append-only trail). Approve
  and deny commit the grant, the status change and the audit row in one
  transaction, so a decision that cannot be recorded does not happen.

## Operational notes

- Approvals live in the same process, Postgres and admin port as the
  rest of the plane. Nothing new to deploy; re-run `make plane` to roll
  a new image, and migrations run idempotently at startup.
- Grant rows are never deleted. Exhausted and expired grants stay
  visible in `make grants` as `live=no` history. Demo-durable via the
  PVC, like the ledger.
- `make up` preserves a governed agent's modelConfig and gateway wiring
  across re-runs, so a grant demo survives a re-provision.
  `make use PRESET=ollama` and `make ungovern-tools` are the explicit
  ways back.

## Limitations

- **The queue is CLI-only.** No routing to Slack, email or a ticket;
  nobody is notified that a request is waiting.
- **"Who approved" is the admin bearer token, not a person.** There is
  no per-approver identity.
- **A grant does not widen the agent's own `toolNames`.** For a kagent
  agent, a granted tool becomes usable through discovery, and what the
  agent *sees* lags: enforcement is immediate, but the agent's
  discovered tool list updates on kagent's next RemoteMCPServer
  reconcile. [slack.md](slack.md) explains this in full, because it is
  where the lag shows up on the demo path.
- **A burned use does not guarantee a delivered result**, since the use
  is consumed before the forward. The audit row says what happened.

The single table of what is and is not governed across the whole plane
is in [README.md](README.md#what-is-governed-today-and-what-is-not).
