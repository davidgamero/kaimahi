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
  [AMOUNT=…]` or `make deny ID=…`; or, from Slack, `@kaimahi approve <id>`
  ([below](#deciding-from-slack)). A decided request is immutable; fresh
  denials file fresh requests.
- **Inspect**: `make grants` (liveness computed by the same predicate
  enforcement uses) and `make approval-audit` (requested / approved /
  denied, with bounds and **who decided**: `admin` for the CLI, `slack:<user
  id>` for a Slack command; the approvals' own append-only trail). Approve
  and deny commit the grant, the status change and the audit row in one
  transaction, so a decision that cannot be recorded does not happen.

## Deciding from Slack

The queue is no longer CLI-only. When a request is filed, by any of the
sites that file one (a gateway tool denial, a budget-cap denial, the
inbound door, `make request`), the plane posts into the pinned Slack
channel that a decision is waiting, and an approver decides it from
Slack, as themselves:

```text
  hello-slack ──▶ gateway: conversations_add_message DENIED, request filed
                     │
                     ▼  (the plane's OWN credential, through its own gateway)
  #channel  ◀── "Kaimahi approval request `3f1c…`: credential hello-slack was
                 denied tool conversations_add_message. To decide, mention
                 the bot: @kaimahi approve 3f1c… [uses=N] [ttl=15m] …"
  approver  ──▶ "@kaimahi approve 3f1c9a2e uses=1 ttl=15m"
                     │ signature ✓, channel ✓, approver list ✓
                     ▼
  #channel  ◀── (in the thread) "approved request 3f1c…: grant 7b0d… uses=1
                 expires=…"          make grants → decided by slack:U…
```

**The notification** goes through the governed posting path. The plane
holds a credential of its own, `kaimahi-plane`, issued by `make
notify-slack` the way `make govern-slack` issues the agent's, and
allowlisted to the posting tool only. That is configuration rather than
a grant, because the plane is the trust root; what it buys is that the
plane's own message is authenticated, allowlisted, pinned to the one
channel (the same Secret key that restricts the MCP server and bounds
the inbound hook) and **audited** like any agent's: `make tool-audit
CRED_TOOLS=kaimahi-plane` shows a row per attempt. The message carries
the request id, the credential, the kind and subject, and the command to
type. It is asynchronous and a convenience: a post that fails never
un-files the request. A refusal the plane can see (the gateway refused
before forwarding, the gateway could not be dialled, Slack said no) is
retried, three attempts in all; anything that may have happened after
the post went out (a timeout, a reset, the gateway's own 502, which it
answers for a dial failure and for a reset after delivery alike) is
recorded and **not** retried, because a notification posted twice is
the double-post the rest of this repo is careful to avoid, and `make
approvals` is always there.

**The command.** `@kaimahi approve <id> [uses=N] [ttl=D] [amount=N]` or
`@kaimahi deny <id>`, as an `app_mention` on the existing `slack-events`
hook ([inbound.md](inbound.md#slack-events-the-loop)). `<id>` is the
request id or its first block (8 characters; a longer prefix if that is
ambiguous). The command is recognised after Slack's signature and the
channel allowlist have been checked and *before* the grant gate: deciding
a request needs no inbound grant (or approving would need an approval),
runs no agent, and spends nothing. Bounds omitted get the hook's defaults,
**one use and fifteen minutes** (`slack_default_uses` /
`slack_default_ttl` in the hook table): an approval typed into a chat is
the least deliberate form an approval takes, so its default is the
tightest grant that still lets the retried action through. `uses=` or
`ttl=` on the command wins; a budget request needs `amount=`; the store
still refuses a grant with no bound at all. A decided request is
immutable: the same command again answers "already approved by
slack:U… at …". The outcome is posted back in the mention's thread
through the same governed path.

**Who may decide** is a Secret-mounted file of Slack user ids,
`make slack-approvers` (stdin only: a user id is a workspace identifier
and never lands in this repo), named in the hook table as
`slack_approvers_file` next to the channel file and read per command.
Channel membership is not authority: anyone in the room can ask the
agent a question; only a listed user can decide. A command from anyone
else is refused (403) and audited with their id; a list that is missing,
empty or malformed fails every command closed (503) and changes nothing
about questions. A bot-authored command, including the plane's own
notification arriving back as an event, is ignored by the loop guard
before it is ever parsed.

**Identity.** `decided_by` is on the request, the grant and the audit
row: `admin` for the CLI (the admin bearer, the only writer that port
admits) and `slack:<user id>` for Slack. `make grants` and `make
approval-audit` show it.

```sh
make slack-approvers          # paste the approver ids on stdin
make notify-slack             # the plane's own credential, posting tool only
# then, in the channel, after a denial has been announced:
#   @kaimahi approve 3f1c9a2e uses=1 ttl=15m
make grants                   # decided by slack:U…
make approval-audit
make inbound-audit HOOK=slack-events   # the command row, and any refused ones
```

On a kind cluster there is no Slack to type into; `make slack-mention
SLACK_USER=U0EXAMPLE COMMAND='approve 3f1c9a2e'` fires a correctly signed
synthetic mention at the hook the way `make inbound-fire` does, which is
how CI asserts the whole path keyless. The notification's post then
answers 502 (the Slack upstream does not exist there), once, and its
audit row under `kaimahi-plane` is the proof it went through the
governed path.

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

- **Routing is Slack only.** No email or ticket routing; a request is
  announced in one pinned channel, and only when `make notify-slack` has
  issued the plane's credential. The notification is best-effort by
  design (a known refusal retried three times, an ambiguous failure
  recorded and left); `make approvals` is the queue of record.
- **Identity is a Slack user id, not a name.** `decided_by` records
  `slack:<user id>`; display names are not resolved, and the CLI path
  records `admin`, since the admin bearer is not a person.
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
