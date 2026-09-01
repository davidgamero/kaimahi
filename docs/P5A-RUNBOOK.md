# P5a runbook — the governed Slack path

Everything the plane governs up to P4c protects an agent that lists
ConfigMaps. Nothing in that demo *needs* governance. P5a fixes that:
the agent posts into a Slack channel humans read, which is the first
genuinely consequential action in this repo.

The deliverable is not "Slack works". It is this cycle:

```text
  agent / MCP client            plane                          human
    │ "post this to Slack"        │                              │
    │ ─── no posting tool ──────▶ │ the agent's credential        │
    │ ◀── "I can't do that" ───── │ projects 1 of the 14 tools    │
    │                             │ the server offers             │
    │ conversations_add_message   │                              │
    │ ──────────────────────────▶ │ DENIED (not allowlisted)     │
    │ ◀──── JSON-RPC -32001 ───── │ + approval request FILED     │
    │       "request filed"       │                              │ make approvals
    │                             │ ◀─── make approve ID=… ───── │ TTL=15m USES=1
    │                             │ the grant enters the          │
    │ ◀── tool now discoverable ─ │ projection: the capability    │
    │ conversations_add_message   │ APPEARS for the agent         │
    │ ──────────────────────────▶ │ ADMITTED via grant ──────────┼──▶ Slack
    │ ◀──── "Posted." ─────────── │ (use consumed, audited)      │   (a human
    │ "post another one"          │                              │    reads it)
    │ ──────────────────────────▶ │ grant spent → not live →     │
    │ ◀── "I can't do that" ───── │ the capability DISAPPEARS    │
```

The connector is the payload. The approval gate is the point.

Note the shape, because it is stronger than "the call is refused": under
P4b's *visible = callable* rule, an approval does not merely permit an
action the agent was already reaching for — it **materialises the
capability**, and its expiry takes the capability away again. See
[Why the agent is never the one denied](#why-the-agent-is-never-the-one-denied).

> **⚠️ This is the first component in the repo with deliberate INTERNET
> egress.** See [What the internet-egress pod
> means](#what-the-internet-egress-pod-means) — it is the strongest
> argument yet for the still-unbuilt NetworkPolicy work, which is out of
> scope here.

## Survey first: what we did not build

Kaimahi writes **no connector code** (prime directive). Slack MCP servers
already exist; kagent already deploys MCP servers. What was surveyed
(2026-09-01, verified against the npm registry, GHCR and the running
image — not from documentation alone):

| Candidate | Verdict |
|---|---|
| Slack's own hosted MCP server (`https://mcp.slack.com/mcp`, [docs](https://docs.slack.dev/ai/slack-mcp-server/)) | **Rejected for this lane.** Not self-hostable, and it authenticates with confidential OAuth 2.0 **user** tokens from a registered, published-or-internal Slack app. A headless agent posting as a bot is not the shape it serves — and a hosted endpoint would make the *gateway's* upstream internet-facing, which is exactly what P4b deferred. Revisit if the approval-routing lane ever needs to act as a person. |
| [`@modelcontextprotocol/server-slack`](https://www.npmjs.com/package/@modelcontextprotocol/server-slack) (the reference server) | **Rejected.** Repo archived 2025-05-29 ([servers-archived](https://github.com/modelcontextprotocol/servers-archived/tree/main/src/slack)); npm marks it *"Package no longer supported"*, latest `2025.4.25` with no publish since 2025-04-25. A deprecated package is not something to hand a workspace token. Its tool vocabulary (`slack_post_message`, `slack_list_channels`, …) survives in forks. |
| `@zencoderai/slack-mcp-server`, `ubie-oss/slack-mcp-server`, and assorted `@mseep/*` forks | **Rejected.** Forks of the archived reference lineage. zencoderai has a single `0.0.1` publish (2025-07-16); ubie-oss publishes only to the GitHub npm registry (needs a PAT). None is maintained enough to hold a workspace token. |
| [`korotovsky/slack-mcp-server`](https://github.com/korotovsky/slack-mcp-server) | **Chosen.** MIT, ~1.8k stars, actively maintained (npm `1.3.0`, 2026-05-14; repo pushed 2026-07-16). Runs as a long-lived container serving **streamable HTTP** — verified in-cluster, not just documented — so the gateway relays to it with no `npx` fetch at pod start. Ships a multi-arch image on GHCR. |

A note on method, since a survey is only as good as its sourcing: the
maintenance status, versions and digest above were read from the npm
registry API, the GHCR registry API and the running image itself, and
were cross-checked against an independent web survey run in parallel.
Where the two disagreed, the measurement won — see the
`SLACK_MCP_API_KEY` finding below, which documentation and the parallel
survey both got wrong.

Provenance and pinning, because this is third-party code that holds a
Slack workspace token:

- Pinned **by digest**, not by tag:
  `ghcr.io/korotovsky/slack-mcp-server@sha256:35cbc988d9282409e27b755957e48a6096fcf037dee72118e97177fe38b1a1b3`
  (the multi-arch index for `v1.3.0`). A tag can be moved; the bytes that
  run must be the bytes that were reviewed. CI asserts the manifest is
  digest-pinned.
- Why it is trusted enough: MIT, source public, an active maintainer, and
  — decisively — it never needs to be trusted with more than we give it.
  It runs in its own pod in the plane's namespace with a bot token scoped
  to one private channel, its posting tool is restricted server-side to
  that channel ID, and every call the agent makes to it is allowlisted
  and audited by the gateway.
- **Honest caveat.** This project's headline feature is a "stealth mode"
  that authenticates with a user's browser session cookies (`xoxc`/`xoxd`)
  to avoid needing workspace-admin approval. Kaimahi deliberately does
  **not** use that path: P5a uses a proper `xoxb` bot token, and
  `scripts/slack-secret.sh` refuses anything else. A bot acts as itself;
  a session token acts as a person.

Net-new in this lane: CRD data, one credential-injection change in the
plane, thin make/script glue, docs, CI. No MCP runtime, no Slack client.

## Architecture

```text
namespace kagent                          namespace kaimahi
┌──────────────────────┐                 ┌──────────────────────┐
│ hello-slack agent    │  Authorization: │ kaimahi-proxy pod    │
│  modelConfig:        │  kmh_… (Secret- │  :8080 LLM proxy     │
│   governed-copilot ──┼──▶ resolved via │  :8081 MCP GATEWAY   │
│  tools[] ──▶ kaimahi-│  headersFrom)   │  authn → scope →     │
│   slack RemoteMCP ───┼────────────────▶│  allowlist/grant →   │
└──────────────────────┘                 │  relay → audit       │
                                          └──┬────────────┬──────┘
  the agent holds ONLY a kmh_ token          │            │
  no Slack token exists in this namespace    │            │ injects
                                              ▼            ▼ SLACK_MCP_API_KEY
                              ┌──────────────────┐  ┌──────────────────────┐
                              │ kagent-tools     │  │ kaimahi-slack-mcp    │
                              │ (P4b upstream)   │  │ MCPServer, digest-   │
                              └──────────────────┘  │ pinned, xoxb token   │
                                                     │ via envFrom Secret   │
                                                     └──────────┬───────────┘
  BOTH tool upstreams stay in-cluster, so P4b's                 │ INTERNET
  deferral of the hardened dialer / SSRF set still holds.       ▼
  This POD, however, talks to the internet.              api.slack.com
```

- **Placement**: the Slack MCP server runs in the **plane's** namespace,
  not the agent's. kagent reconciles an `MCPServer` in any namespace
  (verified on the live cluster), so the workspace token sits next to the
  Copilot key in `kaimahi`, and `kagent` holds nothing but opaque `kmh_`
  tokens.
- **Upstream table**: `k8s/plane/upstreams.yaml` gains a second
  `tool_upstreams` entry, `slack`. CI asserts both entries resolve to
  in-cluster hostnames — an internet-facing *gateway upstream* would
  require the deferred SSRF set and must not slip in silently.
- **No ungoverned Slack path exists.** P3/P4b keep an ungoverned tools
  wiring for contrast; this lane ships none. The only route to the Slack
  server is through the gateway.

## Credential custody

Three secrets, split so no pod holds more than its job needs:

| Secret (ns) | Holds | Reaches |
|---|---|---|
| `kaimahi-slack-bot` (kaimahi) | `SLACK_MCP_XOXB_TOKEN`, `SLACK_MCP_ADD_MESSAGE_TOOL` (the channel ID) | the **MCP server pod only** |
| `kaimahi-slack-mcp-key` (kaimahi) | `SLACK_MCP_API_KEY` | the MCP server pod **and** the proxy, which injects it upstream |
| `kaimahi-slack-token` (kagent) | the agent's `kmh_…` opaque token | the **agent only** |

- The bot token never appears in YAML, argv, env listings or logs.
  `spec.deployment.env` in `k8s/slack-mcp.yaml` is plaintext YAML and
  carries only host/port/log-level; everything secret arrives through
  `secretRefs`, which kagent renders as `envFrom.secretRef` (verified
  against the live 0.9.12 CRD). CI fails if a secret-capable key appears
  in that plaintext map.
- The **channel ID is never committed**. It is workspace-identifying, so
  it rides the Secret and is passed per task at demo time.
- `scripts/slack-secret.sh` captures the token stdin-only into a 0600
  file, checks `auth.test`, and — the board's outward-facing rule
  enforced in code — **refuses to store anything** unless
  `conversations.info` says the channel `is_private` and the bot is a
  member. It also refuses a non-`xoxb` token.

Least-privilege bot scopes for this demo: `chat:write` (post),
`groups:read` (prove the channel is private), `groups:history` (the
read-only tool), `users:read` (name resolution). Note that
`chat:write.public` — which Slack offers by default — lets a bot post to
**any** public channel without being invited; drop it.

## Run it

```sh
make plane                                   # P4a/P4b/P4c plane (image kaimahi-proxy:p5a)
make plane-copilot-secret                    # the demo model runs governed Copilot (D14)
make slack-secret SLACK_CHANNEL=C0XXXXXXXXX  # stdin-only; refuses a non-private channel
make slack-mcp                               # the digest-pinned server, in-cluster
make govern-slack                            # kmh_ credential + READ-ONLY allowlist + agent
```

After `make govern-slack` the credential may call `conversations_history`
and nothing else. `make tool-allowlist CRED_TOOLS=hello-slack` shows it;
the gateway projects it onto `tools/list`, so the agent cannot even see
a posting tool.

### The demo

```sh
# 0. What the agent can even see: the server offers 14 tools; the
#    credential projects one. Posting is not among them.
kubectl -n kagent get remotemcpserver kaimahi-slack \
  -o jsonpath='{.status.discoveredTools[*].name}'      # conversations_history

# 1. Ask the agent to post. It cannot: there is no posting tool in its
#    hands to call.
make slack-post SLACK_CHANNEL=C0XXXXXXXXX MESSAGE='Kaimahi governance demo.'

# 2. The gateway's enforcement point, exercised directly — this is the
#    DENIAL that files the request (and what any MCP client that tries
#    the call gets).
UPSTREAM=slack GOVERNED_SECRET=kaimahi-slack-token \
  bash scripts/tool-denial-probe.sh conversations_add_message
make slack-audit          # denied 403, "approval request filed"

# 3. A human looks at the queue and grants a BOUNDED permit.
make approvals            # copy the id (kind=tool, subject=conversations_add_message)
make approve ID=<uuid> TTL=15m USES=1

# 4. The grant is now in the projection, so the capability appears.
#    kagent re-discovers on reconcile; nudge it and roll the agent.
kubectl -n kagent annotate remotemcpserver kaimahi-slack \
  kaimahi.dev/rediscover="$(date +%s)" --overwrite
kubectl -n kagent get remotemcpserver kaimahi-slack \
  -o jsonpath='{.status.discoveredTools[*].name}'      # now includes the post tool
kubectl -n kagent rollout restart deploy/hello-slack

# 5. The agent posts. For real, into a channel a human reads.
make slack-post SLACK_CHANNEL=C0XXXXXXXXX MESSAGE='Kaimahi governance demo.'
make slack-audit          # allowed 200, detail: granted <grant-id>

# 6. The use is spent. Ask again — nothing is posted.
make slack-post SLACK_CHANNEL=C0XXXXXXXXX MESSAGE='And again?'
make grants               # the grant, live=no, uses 1/1
make approval-audit       # requested / approved / denied, with the bounds
```

Nothing in step 5 widens the *configuration*: the static allowlist still
holds one read-only tool throughout. `make slack-allow SLACK_TOOLS=…` is
the config lever, and it is deliberately not what the demo uses — the
point is that a human said yes to one post, for fifteen minutes, once.

## Why the agent is never the one denied

Measured, not assumed: kagent wires an agent only to tools it
**discovered**. Naming an undiscovered tool in `spec.declarative.tools[].toolNames`
does nothing — verified by patching `conversations_add_message` into the
agent's `toolNames` while the allowlist excluded it, and watching the
agent report it had no such tool rather than attempt a call.

Because P4b projects `tools/list` down to what the credential may call
right now, a non-allowlisted tool is invisible, so a kagent agent can
never *produce* the JSON-RPC denial for it. That has three consequences
worth stating plainly:

- **The denial + auto-filing is exercised at the gateway**, by a client
  that attempts the call directly (`tool-denial-probe.sh`, and any
  non-kagent MCP client). This is the real enforcement point, and the
  one CI asserts.
- **For an agent, the guarantee is stronger than refusal**: the
  capability is not in its hands at all. There is no call to smuggle, no
  retry loop, no prompt injection that reaches a tool the credential
  cannot call — a denied tool is not a locked door, it is a door that
  is not in the room.
- **An approval is therefore constructive**: minting a bounded grant puts
  the tool into the projection, kagent re-discovers it, and the agent
  gains the capability for exactly the grant's life. Exhaustion removes
  it again on the next reconcile.

The cost is a discovery lag: enforcement is immediate, but what an agent
*sees* changes only on kagent's next RemoteMCPServer reconcile, which is
why step 4 nudges it. That lag was already recorded as a P4b/P4c known
limitation; P5a is where it becomes visible in the demo path.

`make slack-down` removes the agent, the seam and the server. The
Secrets survive; delete them to revoke.

## What the internet-egress pod means

`kaimahi-slack-mcp` opens connections to `api.slack.com`. That is a
first for this repo, and it is worth stating plainly rather than burying:

- **The gateway's own reach is unchanged.** Both `tool_upstreams` entries
  are in-cluster Services, so P4b's ruling deferring the hardened
  dialer / SSRF set still holds, and CI asserts it keeps holding.
- **Nothing constrains that pod's egress.** There is no NetworkPolicy in
  this repo (a known limitation carried since P4b). The Slack pod can
  reach anything the cluster can reach, and — worse — **any pod in the
  cluster can reach the Slack MCP server's Service directly**, bypassing
  the gateway, its allowlist, its approvals and its audit trail entirely.
- **The mitigation we hoped for does not work.** The plane now supports
  injecting a tool upstream's own bearer credential from proxy-side
  custody (`credential_file` in the upstream table), which would let the
  Slack server reject any caller that did not come through the gateway.
  Measured on the live cluster with slack-mcp-server v1.3.0: it **does
  not enforce** `SLACK_MCP_API_KEY` on its `http` transport — an
  unauthenticated `initialize` and `tools/list` both answered 200, as did
  a wrong bearer; its SSE transport also served an unauthenticated
  stream. The injection is wired, tested and fails closed on our side,
  and it is documented here as not load-bearing today.
- **What actually closes it** is cluster-level NetworkPolicy: default-deny
  egress with an allowance for the Slack pod, and default-deny ingress to
  the Slack pod except from the proxy. That work is out of scope for this
  lane and remains unbuilt. Until it lands, treat the application-layer
  governance here as *enforcement for agents that use the seam*, not as
  containment of a hostile pod.

Blast radius today is bounded by the credential rather than the network:
the bot token carries `chat:write` for one workspace, and the MCP server
itself restricts `conversations_add_message` to the single channel ID in
`SLACK_MCP_ADD_MESSAGE_TOOL`. That is three independent layers before a
message reaches a channel — gateway allowlist/approval, server-side
channel restriction, and Slack's own scopes — but none of them is a
network boundary.

## Governed vs still ungoverned

| Surface | Status |
|---|---|
| Slack posting via `kaimahi-slack` | **Governed** (P5a): posting is not allowlisted — it is approved per use, bounded, and audited with the grant id |
| Slack read tools via `kaimahi-slack` | **Governed** (P4b): per-credential allowlist, projected onto `tools/list`, every call audited |
| The Slack workspace token | **Custodied** (P4a pattern): plane namespace only, stdin-only capture, never in YAML/argv/logs; the agent holds an opaque `kmh_` token |
| The demo agent's LLM spend | **Governed** (P4a): governed Copilot preset — budgets, ledger, and the real Copilot token held by the proxy |
| LLM calls via ungoverned presets; tool calls via direct `kagent-tool-server` wiring | Ungoverned, by explicit choice. There is **no** ungoverned Slack path |
| **Pod-level network egress (NetworkPolicy)** | **Not built** — and now materially worse than it was: the Slack pod egresses to the internet, and any pod can reach it directly. See above |
| The Slack MCP server's own endpoint auth | **Not effective** — the plane injects a credential the server (v1.3.0) does not enforce on its http transport |
| Internet-facing *gateway* upstreams | **Still not built** — both tool upstreams remain in-cluster; going internet-facing needs the deferred hardened dialer/SSRF set |
| Approval routing (who approved, notified where) | **Not built** — the queue is CLI-only; the approver identity is the admin bearer, not a person |
| What an agent *sees* after a grant | **Lagging, not wrong** — enforcement is immediate; kagent's discovered tool list updates on its next RemoteMCPServer reconcile (P4b limitation, now on the demo path) |

## What CI covers — and what it cannot

CI is **keyless** and stays that way (public, fork-exposed repo): no
Slack token, no Copilot token, ever. The Slack MCP server is therefore
**not deployed in CI**. Rather than stand up a fake Slack to paper over
that, the boundary is made structural: the gateway decides *before* it
forwards, so the whole approval cycle runs, the gateway then **dials**
the committed `slack` upstream, and the call answers **502** because no
such Service exists. `scripts/tool-admit-probe.sh` **fails on a 200** —
CI cannot silently start reaching a tool server — and it fails on any
503 that is a pre-forward denial wearing a 503 (a Postgres blip must
never read as "admitted").

Be precise about what that proves. It proves the allowlist and grant
decisions, and that an admitted call is actually forwarded. It does
**not** validate the upstream URL itself: any unreachable URL answers
502 alike. CI needs a throwaway upstream credential in the plane for the
dial to happen at all — it is not a Slack credential, and no Slack token
exists in CI.

CI asserts:

- the Slack manifests are valid against the live kagent CRDs;
- the committed wiring carries no credential: no secret-capable key in
  plaintext `env`, exactly the two expected `secretRefs`, a
  digest-pinned image, a single Secret-resolved `headersFrom`, no
  `xox[bpca]-` token shape anywhere in the tree, and **posting absent
  from the committed allowlist**;
- the gateway's upstream table has both entries and both are in-cluster;
- the agent-side Secret holds only a `kmh_…` opaque token;
- the full cycle over the `slack` upstream: post **denied 403** and a
  request auto-filed → bounded approval → **admitted**, audited
  `allowed … granted <id>` → use exhausted → denied again.

CI does **not** cover: the Slack MCP server pod itself, its runtime
token custody, the gateway relaying a real Slack response, or Slack
accepting the message. Those are live-verified in the PR, on a cluster
with a real bot token, and nowhere else.

## Operational notes

- The demo agent runs `governed-copilot` (D14). `qwen2.5:3b` is
  documented failing at both halves of this task — composing a message
  and calling a tool (P1/P3 deltas) — so the ollama path is CI's, not
  the demo's.
- The proxy image tag moves with the phase (`kaimahi-proxy:p5a`);
  re-run `make plane` to roll it. `imagePullPolicy: Never` means a
  same-tag rebuild needs the restart `make plane` already does.
- The Slack MCP server runs with `--no-cache` deliberately: its optional
  user/channel caches would pull a directory of the whole workspace into
  the pod. Tools are addressed by channel ID. `channels_list` is
  consequently not useful and is not allowlisted.
- **A burned grant does not guarantee a delivered message.** A tool-grant
  use is consumed *before* the forward (P4c's accepted conservative
  direction), so if the plane cannot read the Slack server's upstream
  credential — the Secret deleted, or the proxy rolled before it existed
  — the call is refused 503 and the audit row reads `allowed 503 granted
  <id>`: the human's approval is spent and nothing was sent. The audit
  trail says so plainly; re-approve after fixing the Secret.
- Rotating the bot token: re-run `make slack-secret`, then
  `kubectl -n kaimahi rollout restart deploy/kaimahi-slack-mcp` (the
  server reads its env at start). The gateway key is generated once and
  kept, since rotating it under a running server would break injected
  calls until both sides roll.
