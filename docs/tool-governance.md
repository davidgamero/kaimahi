# Governing tool calls: the enforcing MCP gateway

Assumes the governance plane is deployed (`make plane`, see
[spend.md](spend.md)) and the tools agent exists ([tools.md](tools.md)).

Spend governance controls what an agent *spends*; this controls what it
*does*. The gateway mounts at kagent's tool-server seam, the
RemoteMCPServer URL, and every MCP call a governed agent makes is
authenticated, scope-checked, allowlist-enforced, and audited before it
reaches a tool server. kagent still runs the tools. Kaimahi ships no MCP
runtime; the gateway relays the protocol and enforces.

> **What this is, and is not.** The allowlist here is static policy. The
> consent flow on top of it, where a denial files an approval request
> and a human mints a bounded grant, is [approvals.md](approvals.md).
> And the gateway is an *application-layer* egress rule: a pod that
> ignores its RemoteMCPServer wiring can still open arbitrary
> connections. Cluster-level NetworkPolicy is not built as of this doc;
> a parallel lane is building it.

## Architecture

```text
namespace kagent                             namespace kaimahi
┌──────────────────────┐  Authorization:   ┌─────────────────────┐
│ hello-tools agent    │  kmh_… (Secret-   │ kaimahi-proxy pod   │
│   tools[] ──▶ kaimahi-│  resolved via    │  :8080 LLM proxy    │   committed
│   tools               │  headersFrom)    │  :8081 MCP GATEWAY ─┼──▶ tool_upstreams
│ RemoteMCPServer       │ ───────────────▶ │  authn → scope →    │   table
│   url = gateway       │                  │  allowlist → relay  │        │
└──────────────────────┘                  │  → audit            │        ▼
                                           └─────────┬───────────┘  http://kagent-tools
kagent controller discovery                          │            .kagent:8084/mcp
(initialize + tools/list) rides            ┌─────────▼──────────┐  (chart-managed,
the same path — discoveredTools            │ Postgres 16        │   read-only lockdown)
IS the allowlist projection                │ tool_allowlist,    │
                                           │ tool_audit         │
                                           └────────────────────┘
```

- **Placement.** The gateway is a second data listener (`:8081`) in the
  existing `kaimahi-proxy` process. It reuses the plane's Postgres pool,
  credential model, log redactor and fail-closed machinery, and adds
  zero CPU requests (the CI node runs nearly full). A dedicated Service,
  `kaimahi-mcp-gateway`, gives the tool seam its own address. The admin
  port stays off every Service.
- **The seam.** [`k8s/kaimahi-tools.yaml`](../k8s/kaimahi-tools.yaml) is
  a Kaimahi-owned RemoteMCPServer whose URL is the gateway. The
  chart-managed `kagent-tool-server` is neither shadowed nor mutated.
  This is a second, governed front door.
- **Upstreams.** The `tool_upstreams` table in
  [`k8s/plane/upstreams.yaml`](../k8s/plane/upstreams.yaml) lists the
  only places the gateway will relay to. Two entries, both in-cluster:
  `kagent-tools` (kagent's tool server) and `slack` (the server deployed
  in [slack.md](slack.md)).

## Credential custody

`make govern-tools` mints a separate `hello-tools` credential. The
agent-side Secret `kagent/kaimahi-tools-token` holds only the `kmh_…`
opaque token (the plane stores its sha256), and the RemoteMCPServer sends
it via `headersFrom`. The gateway strips it, and every other
credential-slot header, before relaying, so the token never reaches a
tool server. The kagent tool server is in-cluster and unauthenticated. A
keyed tool server gets its real credential injected from proxy-side
custody, exactly like the LLM upstreams; the Slack server is the first
one wired that way.

## From zero

```sh
make up             # cluster, ollama, kagent, agents
make plane          # proxy + gateway + Postgres
make govern-tools   # credential, allowlist, gateway wiring for hello-tools
make chat AGENT=hello-tools TASK='What pods run in the ollama namespace?'
make tool-audit     # the call you just made, in the audit trail
```

`make ungovern-tools` restores the direct, ungoverned wiring by
re-applying `k8s/tools-agent.yaml`. Re-run `make plane` after editing
`upstreams.yaml`: the config is read at boot, and the ConfigMap mounts
via subPath, which never live-updates.

## Enforcement, all fail-closed

- **Egress.** Only `tool_upstreams` entries are reachable. Any other
  upstream name answers 403 before any network contact.
- **Protocol scope.** Tools only. `initialize` and
  `notifications/initialized` (the mandatory lifecycle handshake) relay;
  `tools/list` and `tools/call` relay under governance; `ping` is
  answered locally without upstream contact; **every other method is
  denied, not relayed** (JSON-RPC error, audited). JSON-RPC batches are
  rejected outright, since a batch could smuggle a denied method.
- **Allowlist.** Enforced on `tools/call` and **projected** onto
  `tools/list`. kagent's controller discovers through the gateway, so
  `status.discoveredTools` on `kaimahi-tools` shows exactly what the
  credential may call, and the agent never sees the rest. **Empty or
  missing allowlist means nothing is callable.** The one governed
  exception is a live bounded grant ([approvals.md](approvals.md)), which
  admits calls and joins the projection while it lasts. Allowlist edits
  enforce immediately on calls; the projection an agent *sees* refreshes
  on kagent's next RemoteMCPServer reconcile.
- **Audit.** Every `tools/call` outcome and every attributable denial is
  appended to `tool_audit` (credential, upstream, method, tool, decision,
  status). Pre-auth 401/503 refusals have no credential to attribute.
  Allowed rows are written after the response they describe, and **a
  failed audit write trips the gateway to 503 for all subsequent
  traffic** until a write succeeds. Unrecordable actions must not keep
  happening.
- **Auth.** Same as the LLM proxy: unknown token 401, credential store
  unreadable 503, no upstream contact either way.

## Changing the allowlist, and watching a denial

```sh
make tool-allow TOOLS=k8s_get_resources,k8s_get_events   # widen
make tool-allow TOOLS=-                                  # nothing callable
make tool-allowlist                                      # show
bash scripts/tool-denial-probe.sh k8s_describe_resource  # watch a denial
```

The denial probe calls a non-allowlisted tool with the governed token
and requires the JSON-RPC `-32001` "not permitted" error. The attempt
lands in `make tool-audit` as a `denied 403` row.

Two levers, and they differ: `make govern-tools TOOLS=…` sets the
gateway allowlist **and** keeps the agent's `toolNames` aligned with it.
`make tool-allow` alone changes only the gateway policy. Re-run
`govern-tools` (or widen `toolNames` yourself) if the agent should *use*
newly allowed tools. The allowlist is the governance boundary either
way.

The probe scripts read `GOVERNED_SECRET` (default `kaimahi-tools-token`)
and `UPSTREAM` (default `kagent-tools`) from the environment, and
port-forward on `GATEWAY_PORT` (`18081` for the denial probe). If you
run two clusters at once, see the port note in [aks.md](aks.md).

## Verified how

Asserted keylessly in CI on every PR: the governed tool call through the
gateway with the probe-ConfigMap proof from [tools.md](tools.md), the
`allowed 200` audit row, a non-allowlisted call denied `-32001` and
audited `denied 403`, and custody (the agent-side Secret matches
`^kmh_[0-9a-f]{64}$`, the RemoteMCPServer references the Secret via
`headersFrom` with no inline value, and discovery projects exactly the
allowlist). Additionally live-verified and unit-tested: an empty
allowlist denying even the default tool, and the projection hiding 7 of
the 8 tools the upstream offers.

## Operational notes

- The gateway shares the proxy's lifecycle. `make plane` rebuilds and
  rolls both. The image tag in
  [`k8s/plane/proxy.yaml`](../k8s/plane/proxy.yaml) moves with each
  release of the plane, so a stale side-loaded image can never satisfy a
  newer manifest under `imagePullPolicy: Never`, and `make plane` always
  restarts the deployment so a same-tag rebuild takes effect.
- `make govern-tools` is idempotent and ordered so discovery never sees
  an empty projection by accident: credential → allowlist → the
  RemoteMCPServer (waits Accepted) → agent patch (waits Ready).
- If the RemoteMCPServer sits at `Accepted=False` right after
  `make plane`, the first reconcile raced the proxy rollout. It
  self-heals within a minute, the same behaviour the chart's own server
  shows.
- The allowlist is per-credential, not per-(credential, upstream). With
  two upstreams in the table, a credential's allowlist applies across
  both; scope credentials per upstream (the Slack agent has its own) and
  do not share one credential across upstreams with different tool
  vocabularies.
- The audit trail is demo-durable like the ledger: it survives pod
  restarts via the Postgres PVC, and `make down` destroys it.

## Limitations

- Application-layer only. A pod that bypasses the gateway is not
  constrained by it; NetworkPolicy is unbuilt as of this doc.
- Both committed upstreams are in-cluster. An internet-facing gateway
  upstream needs a hardened dialer and SSRF protection that do not exist
  yet.
- Discovery lag: enforcement is immediate, but what an agent sees changes
  on kagent's next reconcile.
- The consolidated status of every governed and ungoverned surface is in
  [README.md](README.md#what-is-governed-today-and-what-is-not).
