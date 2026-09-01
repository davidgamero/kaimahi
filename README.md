# kaimahi

> **Incubation project.** An idea being worked out in the open. Phases 1–3
> run and are verified on every commit; the CLI front door and the
> governance plane are proposals, not products. The name is provisional —
> see [docs/NAMING.md](docs/NAMING.md).

**Scaffold an agent onto Kubernetes from your terminal.** The agent is a YAML
file. The interface is a command. No dashboard, no SaaS account, no runtime
to install into your app.

## CLI-first

CLI before UI, deliberately. A command has an exit code, runs in CI, works
over SSH, pipes into other commands, and can be read in a code review. Every
step of the journey — provision, deploy, converse, switch models, add tools,
tear down — is one.

**Where this is going.** One command that does the heavy lifting, run
without cloning anything:

```bash
npx kaimahi create agent support-triage \
  --model anthropic \
  --instructions ./triage.md \
  --tools kagent-tool-server:k8s_get_resources \
  --out k8s/
```

It generates the agent-as-code YAML — Agent, ModelConfig, tool wiring, and
the Secret *references* to go with them — validates it (server-side dry-run
when a cluster is reachable), and prints the next command. You get a
reviewable file, not a black box: the artifact is the same YAML you would
have hand-written, and it is yours from that point on.

That is the whole reason to build a CLI at all. A Makefile requires a clone;
`npx` does not. Consumption without a clone is the property being chased.

> **`kaimahi create` is proposed, not built.** No package is published and
> the name is unclaimed. The design, a survey against kagent's own CLI, and
> the security model are in [docs/CLI-PROPOSAL.md](docs/CLI-PROPOSAL.md) —
> including the honest case *against* building it. Everything below this
> line works today.

**Where it is now.** The same journey, from a clone, via make:

```bash
make up     # cluster + model server + kagent + agents   (~5-10 min, no API key)
make chat   # talk to it
```

| Consume it as | How |
|---|---|
| **Local dev** | `make up` on kind; keyless, free, offline-capable model |
| **Any conformant cluster** | the manifests are plain CRDs; **AKS** is the named managed target |
| **CI / automation** | the same targets run headless — this repo's [CI](.github/workflows/ci.yml) boots a cluster and asserts a real reply, and a real tool call, on every PR |
| **Your own repo** | copy `k8s/` + the make targets; each agent is one YAML file |
| **Existing kagent install** | `kubectl apply -f k8s/hello-world.yaml` — no kaimahi runtime required |

Agents run on [kagent](https://kagent.dev) — declarative Kubernetes agents
whose Agent CRD YAML *is* the topology artifact. kaimahi is thin glue over
`kind`, `helm`, `kubectl`, and kagent's own CLI. kagent already ships a CLI
and a dashboard; kaimahi does not rebuild them, and `create` is proposed
only for the gap they leave: scaffolding declarative agent YAML.

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
port-forwards the controller, and invokes the agent. Full walkthroughs:
[P1](docs/P1-RUNBOOK.md) (cluster to conversation),
[P2](docs/P2-RUNBOOK.md) (hosted models), [P3](docs/P3-RUNBOOK.md) (tools).

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

Overridable: `KIND_CLUSTER`, `KAGENT_VERSION`, `MODEL`, `AGENT`, `TASK`.

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
caveats: [docs/P2-RUNBOOK.md](docs/P2-RUNBOOK.md).

## Tools

`hello-tools` reaches kagent's own bundled MCP server, wired through
`spec.declarative.tools` — kagent's native mechanism. Kaimahi ships no MCP
runtime, proxy, or gateway. The server is locked down at three layers: k8s
tools only, `--read-only`, and a get/list/watch ClusterRole that **cannot
read Secrets**, with a single-tool allowlist on top.
Details: [docs/P3-RUNBOOK.md](docs/P3-RUNBOOK.md).

> **Both spend and tools are ungoverned today.** A hosted preset sends every
> conversation to a billed API with no budget or ledger in front of it. A
> tool-enabled agent acts on the world with no egress enforcement, no tool
> permits, no approvals, and no audit trail. The demo's own lockdowns are
> the only limits. That governance is the idea being incubated, and it
> arrives in phase 4.

## What kaimahi would add over raw kagent

kagent answers *"how do agents run on Kubernetes."* Kaimahi asks *"how do
you hand one to a team without regretting it."* Scaffolding is the part that
runs today. The thesis is the governance plane kagent lacks — **none of it
is built; it is what phase 4 mounts:**

- **Budgets and spend metering** — every billed call ledgered, even when the
  surrounding operation fails.
- **Approvals and blast-radius permits** — consequential actions wait for a
  human yes, scoped to what was approved.
- **Credential custody** — provider keys never reach agent pods, YAML, or logs.
- **Egress enforcement** — agents reach only permitted endpoints.
- **Audit** — who ran what, with which model, at what cost.

None of it would fork or wrap the runtime: it mounts at kagent's existing
seams (ModelConfig `baseUrl`, MCP tool server). The delegation journeys that
argue for these five specifically are collected in `docs/SCENARIOS.md`
(proposed separately).

## Status

| # | Phase | State |
|---|---|---|
| 1 | Hello world on Kubernetes | **runs** — `make up && make chat`, verified in CI |
| 2 | Hosted LLM endpoints via ModelConfig | **runs** — presets above |
| 3 | Connectors/tools via MCP | **runs** — `hello-tools`, real tool call asserted in CI |
| 4 | Governance plane at kagent's seams | thesis, not built |
| — | `kaimahi create` CLI | proposed — [docs/CLI-PROPOSAL.md](docs/CLI-PROPOSAL.md) |

Cloud-agnostic (kagent runs on any conformant Kubernetes) with first-class
attention to the Azure path: **AKS** as the managed target, **Azure AI
Foundry** among the model endpoints.

## Development

Work is coordinated through [docs/COORDINATION.md](docs/COORDINATION.md).
Every change lands via a PR to `main` with CI green, and verification claims
are backed by actually running the thing.
