<p align="center">
  <img src="brand/hero.png"
       alt="Kaimahi night worker guarding paths for AI agents"
       width="100%">
</p>

# Kaimahi

> **Incubation project.** Kaimahi is built in public. The README and
> documentation label capabilities as running in CI, demonstrated once,
> schema-valid only, proposed, or unbuilt. The name is provisional — see
> [docs/NAMING.md](docs/NAMING.md).

## Governance for AI agents running on Kubernetes.

Kaimahi builds on [kagent](https://kagent.dev) rather than replacing it. It adds
controls at the model and MCP boundaries for consequential agent work.

### Control model spend

Meter model calls, fail closed on monthly budgets, write every request to a
ledger, and keep real provider credentials away from agent pods.

### Constrain tool calls

Route MCP traffic through an enforcing gateway with explicit upstreams,
per-credential tool allowlists, and an audit trail that includes denials.

### Approve consequential actions

Turn a denied model or tool action into a pending request. Human approval issues
a grant limited by expiry, use count, or both. The exception lapses when its
limit is reached.

<p align="center">
  <img src="docs/assets/architecture.svg"
       alt="A Kubernetes agent routes model calls through the Kaimahi LLM proxy and tool calls through its MCP gateway; bounded approvals can widen either path temporarily">
</p>

Governance is opt-in per agent. The documentation identifies ungoverned paths
and current limitations.

## Quickstart

The working path today:

```bash
make up     # kind cluster + local model + kagent + agents (~5–10 minutes)
make chat   # talk to the default agent
```

The default path needs no API key. It uses an in-cluster Ollama model for a real
agent conversation. Continue with the [getting-started guide](docs/getting-started.md)
or choose a capability from the [documentation index](docs/README.md).

| Prerequisite | Why | Install |
|---|---|---|
| Docker | kind runs Kubernetes in containers | <https://docs.docker.com/get-docker/> |
| kind | local Kubernetes cluster | <https://kind.sigs.k8s.io/docs/user/quick-start/#installation> |
| kubectl | cluster interaction | <https://kubernetes.io/docs/tasks/tools/> |
| Helm | installs kagent | <https://helm.sh/docs/intro/install/> |
| make, curl | glue | your package manager |

```bash
make chat TASK="What are you defined in?"
make chat AGENT=hello-tools TASK="What pods are running in the ollama namespace?"
make govern                              # route the agent's model calls through the governed proxy
make status                              # agents, modelconfigs, pods
make down                                # delete the cluster
```

`make chat` fetches the pinned kagent CLI to `bin/kagent` (checksum-verified),
port-forwards the controller, and invokes the agent.

| Consume it as | How |
|---|---|
| **Local dev** | `make up` on kind; keyless, free, offline-capable model |
| **Any conformant cluster** | the manifests are plain CRDs; **AKS** is the named managed target, and the governance plane has been [run there once](docs/aks.md) |
| **CI / automation** | the same targets run headless — this repo's [CI](.github/workflows/ci.yml) boots a cluster and asserts a real reply, and a real tool call, on every PR |
| **Your own repo** | copy `k8s/` + the make targets; each agent is one YAML file |
| **Existing kagent install** | `kubectl apply -f k8s/hello-world.yaml` — no kaimahi runtime required |

## Status

| # | Phase | State |
|---|---|---|
| 1 | Hello world on Kubernetes | **runs** — `make up && make chat`, verified in CI |
| 2 | Hosted LLM endpoints via ModelConfig | **runs** — presets below |
| 3 | Connectors/tools via MCP | **runs** — `hello-tools`, real tool call asserted in CI |
| 4a | Governed LLM spend (proxy, budgets, ledger, custody) | **runs** — `make govern`, denial + ledger asserted in CI |
| 4b | Governed tool calls (MCP gateway, allowlists, audit) | **runs** — `make govern-tools`, denial + audit asserted in CI |
| 4c | Approvals / time-boxed permits (deny-and-pend, bounded grants) | **runs** — `make approvals`, both cycles asserted in CI |
| 5a | Governed Slack outbound — posting is an approved action | **runs** — `make govern-slack`; the deny → approve → post → burn cycle asserted keyless in CI ([docs/slack.md](docs/slack.md)) |
| 5b | Cluster portability + a real managed-cluster run | **demonstrated once** — the plane, a governed Copilot chat, a ledger row, a budget denial and a governed tool call, all on a real AKS cluster, which was then deleted ([docs/aks.md](docs/aks.md)) |
| 7a | Network policy around the plane | **runs** — default-deny NetworkPolicy in both directions, proven by a probe on every PR ([docs/egress.md](docs/egress.md)) |
| 7b | Inbound hooks (webhooks → agent), governed | **runs** — auth before any work, budget checked at the door, a bounded grant consumed per event, probed keyless in CI; the pre-auth rate limiter and the queue are per replica by design ([docs/inbound.md](docs/inbound.md)) |
| 8 | Approvals routed to Slack, with the approver's identity | **runs** — a filed request is announced in the channel through the plane's own governed post; `@kaimahi approve <id>` from a listed approver mints the grant in their name, asserted keyless in CI with signed synthetic mentions; live verification on AKS pending ([docs/approvals.md](docs/approvals.md#deciding-from-slack)) |
| 9 | Run it for real: two stateless replicas, exact budgets, metrics | **runs** — two replicas behind every seam, every budget and grant decision serialized per credential in Postgres (N concurrent calls against a cap with room for one admit exactly one, asserted across both replicas in CI), a replica killed mid-cycle and Postgres restarted without a proxy restart, migrations under a lock, Prometheus on its own port, `make backup` / `make restore` ([docs/operations.md](docs/operations.md)) |
| — | `kaimahi agent create` CLI | considered and prototyped, not built — [docs/CLI-PROPOSAL.md](docs/CLI-PROPOSAL.md) |

**Limitations, stated plainly.** Governance is opt-in per agent: an
*ungoverned* preset still bills with no ledger, and an ungoverned tools wiring
still acts with no audit. The plane's namespace is default-deny in both
directions and the Slack pod is the one thing allowed out, on 443 only; the
`kagent` and `ollama` namespaces are not policed. Internet-facing tool
upstreams remain unbuilt; Slack is the only chat route for approvals (the
`make approve` path remains, recording `admin`). The plane runs as two
stateless replicas that agree on every decision in Postgres; Postgres
itself is one replica with `make backup` / `make restore`, not a highly
available database ([docs/operations.md](docs/operations.md)).

Cloud-agnostic — it runs on any conformant Kubernetes — with first-class
attention to the Azure path: **AKS** as the managed target, **Azure AI
Foundry** among the model endpoints. On AKS, be precise about what that means.
It has been **demonstrated, not maintained**: one verified run on 2026-09-01,
then torn down. There is no standing cluster and no Azure credential in CI —
the repo is public and fork-exposed, so CI stays on kind and keyless,
re-proving the portability *logic* (the context guard's decisions, the
registry render) on every PR rather than the cloud itself.

## Documentation

[docs/demo.md](docs/demo.md) is the demo start to finish, with what each
step should print. [docs/README.md](docs/README.md) routes by what you want
to do, and holds the one table of what is governed today and what is not:
[getting started](docs/getting-started.md), [hosted models](docs/models.md),
[tools](docs/tools.md), [spend](docs/spend.md),
[tool governance](docs/tool-governance.md), [approvals](docs/approvals.md),
[Slack](docs/slack.md), [egress](docs/egress.md), [inbound](docs/inbound.md),
[AKS](docs/aks.md).

## Governance in practice

Every control is one make target, and each is asserted in CI.

| Command | Does | Docs |
|---|---|---|
| `make govern` | routes the agent's LLM calls through the in-cluster kaimahi proxy — monthly budgets that fail closed, every call ledgered, the real upstream credential held only by the proxy | [spend](docs/spend.md) |
| `make govern-tools` | puts the tools agent behind the enforcing MCP gateway — a committed upstream table as the egress rule at that seam, a per-credential tool allowlist projected into what the agent can even see, every call audited | [tool governance](docs/tool-governance.md) |
| `make approve` | a denial files an approval request; this mints a bounded permit (expiry and/or use count) that widens exactly what was denied, then lapses | [approvals](docs/approvals.md) |
| `make slack-secret` → `make slack-mcp` → `make govern-slack` | a demo agent behind an in-cluster Slack MCP server (third-party, digest-pinned, deployed by kagent) where **posting is not allowlisted**: denied, requested, granted one bounded use, posted, burned, denied again — all audited | [Slack](docs/slack.md) |
| `make slack-approvers` → `make notify-slack` | the human, reachable where the demo lives: a filed request is announced in the channel by the plane's own governed post, a listed approver answers `@kaimahi approve <id> uses=1 ttl=15m` in Slack, and the grant and its audit rows carry `slack:<their id>` | [approvals](docs/approvals.md#deciding-from-slack) |

It mounts at seams that already exist — the model `baseUrl` and the MCP
tool server — rather than forking or wrapping the runtime. Every call through
the governed preset is ledgered, even denials, even at $0; the agent holds an
opaque kaimahi token and real provider keys never reach agent pods, YAML, or
logs; unbounded grants are refused. The delegation journeys that argue for
these controls are collected in `docs/SCENARIOS.md` (proposed separately).

## Command reference

Commands that exist today. `kaimahi agent create` is deliberately **not** in
this table — it was prototyped and shelved, not built.

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
can name a cluster somebody cares about. Every target that *writes to
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
never mutated. Agents run on kagent — declarative Kubernetes agents whose
Agent CRD YAML *is* the topology artifact. Kaimahi is thin glue over `kind`,
`helm`, `kubectl`, and the kagent CLI.

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
allowlist on top. Details: [docs/tools.md](docs/tools.md).

## A scaffolder CLI: considered, not built

CLI before UI, deliberately: every step of the journey — provision, deploy,
converse, switch models, add tools, tear down — is a command today, from a
clone, via make. A `kaimahi agent create` scaffolder that would do the same
without a clone was surveyed against kagent's own CLI, ruled on, and
prototyped in a pull request that was closed unmerged. No package is
published and the name is unclaimed. The survey, the design, the security
model and the case against building it stay in
[docs/CLI-PROPOSAL.md](docs/CLI-PROPOSAL.md) for whoever picks it up next.

## Development

Start with [CONTRIBUTING.md](CONTRIBUTING.md). Work is coordinated through
[docs/COORDINATION.md](docs/COORDINATION.md). Every change lands via a PR to
`main` with CI green, and verification claims are backed by actually running
the thing.
