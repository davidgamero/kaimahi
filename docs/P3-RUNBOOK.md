# P3 runbook — connectors/tools via MCP

P1's agent talks; P2 lets it think with any hosted model; P3 gives it hands:
a tool it can call, wired through MCP — kagent's native tool mechanism. The
topology stays agent-as-code: one new Agent YAML
([`k8s/tools-agent.yaml`](../k8s/tools-agent.yaml)) and a few lines of helm
values. Tomte builds **no** MCP runtime, proxy, or gateway in this phase.

> **⚠️ P3 tools are ungoverned.** The agent can now act on the world through
> whatever tools it is wired to, with no egress enforcement, no tool
> permits, no approval workflow, and no audit trail in front of the calls.
> The only limits here are the demo's own lockdown (read-only tool server,
> single-tool allowlist). That governance — an enforcing MCP gateway at
> exactly this seam — is Tomte's actual product and arrives in P4.

## What ships, and what was already there

kagent 0.9.12 ships the entire MCP stack (all verified on the live
cluster):

- **`MCPServer`** (v1alpha1) — deploys an MCP server *in-cluster* from a
  container image; stdio transport rides a sidecar gateway that spawns the
  process per session (uvx/npx, 2–8s startup — mind client timeouts), or
  plain HTTP.
- **`RemoteMCPServer`** (v1alpha2) — points at an *existing* MCP endpoint
  (SSE or streamable HTTP).
- **`Agent.spec.declarative.tools[]`** — wires an agent to a server, with
  `toolNames` allowlisting and `headersFrom` for authenticated servers.
- **`ToolServer`** (v1alpha1) — the *legacy* pre-0.9 API (bare transport
  config, no deployment machinery). Still served for compatibility, but the
  supported path at 0.9.12 is MCPServer/RemoteMCPServer: kagent's own chart
  publishes its bundled tool server as a RemoteMCPServer, not a ToolServer.
  New configs should not use it.
- **A bundled tool server** — the chart's `kagent-tools` subchart deploys
  kagent's own MCP server (k8s/helm/istio/… introspection tools, streamable
  HTTP on `:8084/mcp`) and creates the `kagent-tool-server` RemoteMCPServer
  pointing at it.

So P3's tool server is not ours and not third-party: it is kagent's own,
switched on in [`k8s/kagent-values.yaml`](../k8s/kagent-values.yaml) with
every lockdown knob the subchart offers:

- `tools.enabledTools: [k8s]` — the k8s provider only (no helm/istio/argo/
  cilium/prometheus surface);
- `tools.args: [--read-only]` — write tools disabled at the application
  layer (the server logs `Running in read-only mode` and discovers only the
  eight `k8s_*` read tools);
- `rbac.readOnly: true` — a get/list/watch ClusterRole instead of the
  subchart's default cluster-admin, with `allowSecrets` left `false`, so
  the tool server **cannot read Secrets** even if asked.

Alternatives considered (recorded for the PR): an `MCPServer` deploying a
third-party image (e.g. the MCP "everything" server's deterministic `add`
tool — adds a Docker-Hub supply-chain dependency and demos a toy instead of
a useful connector); a stdio server via uvx/npx (pulls packages from the
internet at session start — runtime egress CI can't have); writing our own
tiny MCP server (prime directive violation: net-new runtime machinery).
Reusing kagent's bundled server needs zero new images, zero runtime egress,
and exercises the same CRD path any real connector would use.

## The agent

`k8s/tools-agent.yaml` adds a second declarative agent, `hello-tools`. The
P1 artifact `k8s/hello-world.yaml` is never mutated (same rule P2
followed): the P1 agent stays tool-free, and the P3 diff is a separate,
reviewable YAML. `hello-tools` reuses the P1 `hello-world-model`
ModelConfig (keyless in-cluster Ollama, qwen2.5:3b) and wires exactly one
tool:

```yaml
tools:
  - type: McpServer
    mcpServer:
      apiGroup: kagent.dev
      kind: RemoteMCPServer
      name: kagent-tool-server
      toolNames: [k8s_get_resources]
```

The single-tool allowlist keeps the small local model reliable (one
obvious tool to pick) and keeps the ungoverned surface minimal until P4
permits exist. Alternatives: patching tools into `hello-world` (mutates the
demo artifact, and its system message forbids tool use), or exposing all
eight read tools (nothing stops you — widen `toolNames` — but do it
knowingly).

## Run it

```bash
make up                     # now also enables kagent-tools + applies hello-tools
make chat AGENT=hello-tools TASK='What pods are running in the ollama namespace?'
```

`make up` gained one dependency (`tools-agent`), which waits for the
`kagent-tool-server` RemoteMCPServer to be Accepted (the controller must
connect and discover tools; the first reconcile can race the tool-server
pod and retry for a minute) and then applies the agent. Everything else —
`make chat`, `make use PRESET=…`, `make down` — is unchanged, and the P2
model presets apply to `hello-tools` too if you point its `modelConfig` at
one.

## Verifying a REAL tool call (not a plausible answer)

A Ready agent and a fluent reply prove nothing — a model can guess
"kube-root-ca.crt" without ever touching a tool. The e2e check (CI runs it
keyless on every push) forces a live data round-trip:

1. create a ConfigMap with an unguessable random name
   (`probe-<8 hex chars>`);
2. ask `hello-tools` to list configmaps in that namespace;
3. `scripts/verify-chat.py tool-chat.out k8s_get_resources $probe` then
   requires, fail-closed: A2A `state=completed`, a `function_call` for
   `k8s_get_resources` **and** a successful (`isError: false`)
   `function_response` in the task history, and the random probe name in
   the reply text.

The A2A task history is the invocation evidence — kagent records the tool
call and its response as structured message parts. The tool server's own
log is a second witness:

```text
msg="executing command" command=kubectl args="[get configmap -n default -o wide]"
```

Live verification 2026-08-31: qwen2.5:3b called `k8s_get_resources` with
correct arguments on the first attempt and echoed the probe name back
(`state=completed`). The P1 model pin holds for the tool path — no model
change was needed. One reliability wrinkle surfaced during repeat trials:
the model occasionally invoked the tool correctly, received the probe in
the response, and still *summarized* it away ("There are no configmaps…").
The system message therefore orders it to copy the tool table's NAME column
verbatim and never claim emptiness when rows exist; with that wording the
probe check passed 10/10 consecutive fresh-cluster trials. If you swap in
a different small model, re-run the trials before trusting it in CI.

## Where P4 mounts

Every tool call in P3 flows agent → RemoteMCPServer URL → tool server.
That URL is the governance seam: P4 puts Tomte's enforcing MCP gateway
between the two, so permits, approvals, egress policy, and audit apply to
every connector — including authenticated ones (`headersFrom` + a Secret
captured stdin-only), which P3 deliberately avoids by choosing a keyless
demo tool.
