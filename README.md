# kaimahi

> **Incubation project.** An idea being worked out in the open. Phases 1–3
> run and are verified on every commit, and the governance plane's thesis
> is now delivered in its first full pass — budgets, spend metering, and
> credential custody for LLM calls; an enforcing gateway with allowlists
> and audit for tool calls; and human approvals minting bounded
> permits; default-deny NetworkPolicy around the plane, proven by a probe
> on every PR; and inbound hooks that let the outside world trigger an
> agent, governed the same way. Incubation continues honestly:
> internet-facing tool upstreams and richer approval routing remain
> unbuilt, and the CLI front door is still a proposal. The name is
> provisional — see [docs/NAMING.md](docs/NAMING.md).

**Scaffold an agent onto Kubernetes from your terminal.** The agent is a YAML
file. The interface is a command. No dashboard, no SaaS account, no runtime
to install into your app.

## CLI-first

CLI before UI, deliberately. A command has an exit code, runs in CI, works
over SSH, pipes into other commands, and can be read in a code review. Every
step of the journey — provision, deploy, converse, switch models, add tools,
tear down — is one.

**The front door.** One command that does the heavy lifting, run without
cloning anything:

```bash
npx github:kaimahi-agents/kaimahi agent create support-triage \
  --instructions ./triage.md \
  --tools kaimahi-tools:k8s_get_resources \
  --out k8s/
```

It generates the agent-as-code YAML — Agent, model preset reference, tool
wiring with a mandatory allowlist, and Secret *references* rather than
secrets — validates it (server-side dry run when a cluster is reachable),
and prints the next command. You get a reviewable file, not a black box: the
artifact is the same YAML you would have hand-written, and it is yours from
that point on.

With no `--model` it defaults to a **governed** preset, so a scaffolded agent
is metered, budgeted and audited from its first call.

That is the whole reason to build a CLI at all. A Makefile requires a clone;
`npx` does not. Consumption without a clone is the property being chased.

> **Built, not published.** Per D19 the CLI is internal for now — run it
> straight from the repo as above; publishing to npm waits on D9's naming
> gates. `agent create` is the only command: reading, updating and deleting
> agents stay with `kubectl` and the kagent CLI. Design, survey and security
> model in [docs/CLI-PROPOSAL.md](docs/CLI-PROPOSAL.md); what it does and
> what is verified in [docs/CLI-PROTOTYPE.md](docs/CLI-PROTOTYPE.md).


**Where it is now.** The same journey, from a clone, via make:

```bash
make up     # cluster + model server + kagent + agents   (~5-10 min, no API key)
make chat   # talk to it
```

| Consume it as | How |
|---|---|
| **Local dev** | `make up` on kind; keyless, free, offline-capable model |
| **Any conformant cluster** | the manifests are plain CRDs; **AKS** is the named managed target, and the governance plane has now been [run there for real](docs/aks.md) |
| **CI / automation** | the same targets run headless — this repo's [CI](.github/workflows/ci.yml) boots a cluster and asserts a real reply, and a real tool call, on every PR |
| **Your own repo** | copy `k8s/` + the make targets; each agent is one YAML file |
| **Existing kagent install** | `kubectl apply -f k8s/hello-world.yaml` — no kaimahi runtime required |

Agents run on [kagent](https://kagent.dev) — declarative Kubernetes agents
whose Agent CRD YAML *is* the topology artifact. kaimahi is thin glue over
`kind`, `helm`, `kubectl`, and the kagent CLI; `create` would scaffold that
YAML.

## Quickstart

| Prerequisite | Why | Install |
|---|---|---|
| Docker | kind runs Kubernetes in containers | <https://docs.docker.com/get-docker/> |
| kind | local Kubernetes cluster | <https://kind.sigs.k8s.io/docs/user/quick-start/#installation> |
| kubectl | cluster interaction | <https://kubernetes.io/docs/tasks/tools/> |
| Helm | installs kagent | <https://helm.sh/docs/intro/install/> |
| make, curl | glue | your package manager |

No API key anywhere: the default model is an in-cluster
[Ollama](https://ollama.com) server running `qwen2.5:3b`.

```bash
make up                                  # provision everything
make chat                                # default question
make chat TASK="What are you defined in?"
make chat AGENT=hello-tools TASK="What pods are running in the ollama namespace?"
make status                              # agents, modelconfigs, pods
make down                                # delete the cluster
```

`make chat` fetches the pinned kagent CLI to `bin/kagent` (checksum-verified),
port-forwards the controller, and invokes the agent. The docs are organised
by what you want to do — start at [docs/README.md](docs/README.md):
[getting started](docs/getting-started.md), [hosted models](docs/models.md),
[tools](docs/tools.md).

## Command reference

Commands that exist today. `kaimahi create` is deliberately **not** in this
table — it is a proposal.

| Command | Does |
|---|---|
| `make up` | cluster → Ollama → model pull → kagent → agents → status |
| `make chat [AGENT=… TASK=…]` | one question to an agent via the kagent CLI |
| `make status` | `get agents,modelconfigs` + pods |
| `make down` | delete the kind cluster |
| `make use PRESET=<name>` | point the agent at a model preset from `k8s/models/` |
| `make use-ollama` | back to the keyless in-cluster model |
| `make model-secret NAME=<secret>` | store an API key from **stdin only** |
| `make copilot-secret` | GitHub device login → short-lived Copilot token → Secret |
| `make tools-agent` | apply the MCP tools-enabled agent |
| `make model MODEL=<tag>` | pull another Ollama model (also edit `model:` in the YAML) |
| `make aks-cluster` / `make aks-down` | create / **delete** an ephemeral AKS cluster + private ACR |

Overridable: `KIND_CLUSTER`, `KAGENT_VERSION`, `MODEL`, `AGENT`, `TASK`,
and `TARGET` (`kind` by default, or `aks`).

**Targeting a real cluster.** `KUBE_CTX` is overridable, so `make down`
can now name a cluster somebody cares about. Every target that *writes to
a cluster* therefore prints the context, API-server host and namespaces it
is about to touch, and requires an explicit confirmation naming the
context when that context is not a local kind cluster — fail closed, no
confirmation no action. Read-only targets never prompt, and the Azure
provisioning/teardown targets carry their own gates instead. See
[docs/aks.md](docs/aks.md).

## The artifact: agent as code

[`k8s/hello-world.yaml`](k8s/hello-world.yaml) is the whole agent — the model
it thinks with and the agent itself, in one reviewable document:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: hello-world
  namespace: kagent
spec:
  type: Declarative
  declarative:
    modelConfig: hello-world-model
    systemMessage: |
      You are Kaimahi's hello-world agent, running on Kubernetes via kagent.
      ...
```

`kubectl apply -f` it and the controller provisions the agent. The topology
grows the same way it started — as YAML you can diff:
[`k8s/tools-agent.yaml`](k8s/tools-agent.yaml) is the P1 agent plus a
`tools:` block wiring it to an MCP server, and the P1 artifact itself is
never mutated. This is what `kaimahi create` would generate.

## Model endpoints

Each endpoint is a committed kagent `ModelConfig` preset in
[`k8s/models/`](k8s/models/); one command switches the agent between them.

```bash
make model-secret NAME=anthropic-api-key   # stdin only — never argv, YAML, or logs
make use PRESET=anthropic
make chat
```

| Preset | Endpoint | Secret | Live-verified |
|---|---|---|---|
| `ollama` | in-cluster, keyless, free | — | **yes** (e2e in CI) |
| `github-copilot` | Copilot subscription models | `make copilot-secret` | **yes** (`gpt-5-mini`) |
| `anthropic` | Anthropic API | `anthropic-api-key` | schema-valid only |
| `openai` | OpenAI API | `openai-api-key` | schema-valid only |
| `openrouter` | OpenRouter gateway | `openrouter-api-key` | schema-valid only |
| `azure-foundry` | Azure AI Foundry (v1 GA) | `azure-foundry-api-key` | schema-valid only |
| `openai-compatible` | any OpenAI-compatible base URL | `openai-compatible-api-key` | schema-valid only |

"Schema-valid only" is literal: CI dry-runs every preset against the live
CRDs, but no real completion has been bought through it yet. Details and
caveats: [docs/models.md](docs/models.md).

## Tools

`hello-tools` reaches an MCP server through `spec.declarative.tools`. The
server is locked down at three layers: k8s tools only, `--read-only`, and a
get/list/watch ClusterRole that **cannot read Secrets**, with a single-tool
allowlist on top.
Details: [docs/tools.md](docs/tools.md).

> **Spend, tool calls, and exceptions can now be governed.** `make govern` routes the
> agent's LLM calls through the in-cluster kaimahi proxy — monthly budgets
> that fail closed, every call ledgered, and the real upstream credential
> held only by the proxy ([docs/spend.md](docs/spend.md)).
> `make govern-tools` puts the tools agent behind the enforcing MCP
> gateway — a committed upstream table as the egress rule at that seam,
> a per-credential tool allowlist projected into what the agent can even
> see, and every call audited ([docs/tool-governance.md](docs/tool-governance.md)).
> A denial is no longer a dead end: it files an approval request, and
> `make approve` mints a bounded permit (expiry and/or use count) that
> widens exactly what was denied, then lapses
> ([docs/approvals.md](docs/approvals.md)).
> And it now guards something worth guarding: `make slack-secret` →
> `make slack-mcp` → `make govern-slack` puts a demo agent behind an
> in-cluster Slack MCP server (third-party, digest-pinned, deployed by
> kagent — no connector code), where **posting is not allowlisted**. The agent is denied, a request is filed, a human
> grants one bounded use, the message lands, the grant burns, the next
> attempt is denied again — all of it audited
> ([docs/slack.md](docs/slack.md)).
>
> Governance stays opt-in per agent: an *ungoverned* preset still bills
> with no ledger, and an ungoverned tools wiring still acts with no audit.
> The plane's namespace is default-deny in both directions and the Slack
> pod is the one thing allowed out, on 443 only
> ([docs/egress.md](docs/egress.md)); the `kagent` and `ollama` namespaces
> are not policed.

## The thesis: a governance plane

Getting an agent running is the easy part, and it runs today. The open
question is how you hand one to a team without regretting it. That is the
idea being incubated — phase 4, arriving in slices:

- **Budgets and spend metering** — *built (P4a)*: every call through the
  governed preset is ledgered — even denials, even at $0 — and budgets
  fail closed.
- **Credential custody** — *built (P4a)*: the agent holds an opaque
  kaimahi token; real provider keys live only with the proxy and never
  reach agent pods, YAML, or logs.
- **Approvals and blast-radius permits** — consequential actions wait for a
  human yes, scoped to what was approved. *Built (P4c)*: a denied action
  files an approval request; approving mints a bounded grant (expiry
  and/or use count — unbounded grants are refused) that widens exactly
  the denied surface, then lapses.
- **Egress enforcement** — agents reach only permitted endpoints.
  *Built at the MCP seam (P4b)* — the gateway relays only to a committed
  upstream table, with per-credential tool allowlists — *and at the
  network (P7a)*: default-deny NetworkPolicy on the plane's namespace,
  with a probe that proves the negative in CI.
- **Audit** — who ran what, with which model, at what cost. *Built
  (P4a+P4b)*: the spend ledger and the tool-call audit trail, denials
  included.

It mounts at seams that already exist — the model `baseUrl` and the MCP
tool server — rather than forking or wrapping the runtime. The delegation
journeys that argue for these five specifically are collected in
`docs/SCENARIOS.md` (proposed separately).

## Status

| # | Phase | State |
|---|---|---|
| 1 | Hello world on Kubernetes | **runs** — `make up && make chat`, verified in CI |
| 2 | Hosted LLM endpoints via ModelConfig | **runs** — presets above |
| 3 | Connectors/tools via MCP | **runs** — `hello-tools`, real tool call asserted in CI |
| 4a | Governed LLM spend (proxy, budgets, ledger, custody) | **runs** — `make govern`, denial + ledger asserted in CI |
| 4b | Governed tool calls (MCP gateway, allowlists, audit) | **runs** — `make govern-tools`, denial + audit asserted in CI |
| 4c | Approvals / time-boxed permits (deny-and-pend, bounded grants) | **runs** — `make approvals`, both cycles asserted in CI |
| 5a | Governed Slack outbound — posting is an approved action | **runs** — `make govern-slack`; the deny → approve → post → burn cycle asserted keyless in CI ([docs/slack.md](docs/slack.md)) |
| 5b | Cluster portability + a real managed-cluster run | **demonstrated once** — the plane, a governed Copilot chat, a ledger row, a budget denial and a governed tool call, all on a real AKS cluster, which was then deleted ([docs/aks.md](docs/aks.md)) |
| — | `kaimahi create` CLI | proposed — [docs/CLI-PROPOSAL.md](docs/CLI-PROPOSAL.md) |

Cloud-agnostic — it runs on any conformant Kubernetes — with first-class
attention to the Azure path: **AKS** as the managed target, **Azure AI
Foundry** among the model endpoints.

On AKS, be precise about what that means. It has been **demonstrated, not
maintained**: one verified run on 2026-09-01, then torn down. There is no
standing cluster and no Azure credential in CI — the repo is public and
fork-exposed, so CI stays on kind and keyless, re-proving the portability
*logic* (the context guard's decisions, the registry render) on every PR
rather than the cloud itself.

## Documentation

[docs/README.md](docs/README.md) routes by what you want to do, and holds
the one table of what is governed today and what is not.

## Development

Work is coordinated through [docs/COORDINATION.md](docs/COORDINATION.md).
Every change lands via a PR to `main` with CI green, and verification claims
are backed by actually running the thing.
