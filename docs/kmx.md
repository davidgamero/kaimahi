# `kmx` — one command for creating and running agents

`kmx` is the developer journey as a single Go binary: a cluster, a local
model, kagent, an agent, a conversation, teardown. It needs no clone and no
Makefile.

It is also the only implementation of that journey. The Makefile's `up`,
`cluster`, `ollama`, `model`, `kagent`, `agent`, `tools-agent`, `chat`,
`status` and `down` targets are now one-line recipes that call this binary,
so CI proves the code you actually run. Everything else in the Makefile —
the governance plane, approvals, Slack, GitHub, AKS, the probes — is
unchanged and still make's.

**Status: milestone 1.** Nothing is published. `kmx` is a provisional name,
like `kaimahi` itself, and is not claimed anywhere (D26/D27 on the
[board](COORDINATION.md)).

## Install

```bash
go install github.com/kaimahi-agents/kaimahi/cmd/kmx@main
```

That is the only install path in this milestone: no Homebrew tap, no
release, no npm or PyPI package. Pin a reviewed commit rather than a branch
if you care what you are running — `@<sha>` works the same way.

From a clone, `make bin/kmx` builds the same binary and every `make` target
below uses it.

| Prerequisite | Why |
|---|---|
| Go 1.26+ | to install or build kmx |
| Docker **or** Podman | kind runs Kubernetes in containers |
| kind | the local cluster |
| kubectl | kmx shells out to it for every read and write |
| Helm | installs kagent |

kmx fetches the pinned kagent CLI itself, checksum-verified, the first time
you chat. In a clone it is handed the checkout's `bin/kagent` instead, so
there is one binary on disk.

## The journey

```bash
kmx up                                   # kind + Ollama + the model + kagent + two agents
kmx agent chat hello-world "Who are you?"
kmx status
kmx down
```

## Commands

| Command | What it does |
|---|---|
| `kmx ctx` | print the context kmx will act on, where that came from, and its posture |
| `kmx ctx <context>` | select that context for later commands (recorded in kmx's config directory — `~/.config/kmx/context` on Linux; set `KMX_HOME` to put it elsewhere) |
| `kmx up` | create the kind cluster, deploy Ollama, pull the pinned model, install kagent by helm, apply both agents, wait for each to be Ready, print status |
| `kmx up --step <step>` | one step only: `cluster`, `ollama`, `model`, `kagent`, `agent`, `tools-agent` |
| `kmx agent create <name>` | scaffold `agents/<name>.yaml` and apply it |
| `kmx agent chat <name> [message]` | ask an agent one question, through `kagent invoke` |
| `kmx status` | agents, modelconfigs and pods |
| `kmx down` | delete the kind cluster kmx created |
| `kmx version` | the pinned kagent and model versions |

Reading, updating and deleting agents are not kmx's job — kubectl and the
kagent CLI already do them, and `kmx agent list` says so and prints the
commands. Scaffolding is the only letter of CRUD with a real gap
([CLI-PROPOSAL.md](CLI-PROPOSAL.md) is the survey that established that).

## Settings

kmx reads the Makefile's own variable names, with the Makefile's defaults,
so `KIND_CLUSTER=mine make up` and `KIND_CLUSTER=mine kmx up` are the same
run.

| Variable | Default | Meaning |
|---|---|---|
| `KIND_CLUSTER` | `kaimahi-p1` | the cluster `up` creates and `down` deletes |
| `KUBE_CTX` | `kind-$KIND_CLUSTER` | the context to act on |
| `CONTAINER_ENGINE` | `docker` | `docker` or `podman` (sets `KIND_EXPERIMENTAL_PROVIDER`) |
| `KAGENT_VERSION` | `0.9.12` | pinned kagent chart **and** CLI |
| `MODEL` | `qwen2.5:3b` | model pulled into Ollama |
| `CHAT_PORT` | `8083` | local port for the controller forward |
| `KAIMAHI_CONFIRM` | unset | confirm a non-kind context, by name |
| `KMX_HOME` | `~/.config/kmx` | where the selected context and the cached kagent binary live |

`--context <name>` overrides `KUBE_CTX` for one command.

## Where the command will land

Every mutating command prints where it is about to act, and refuses anything
that is not a local kind cluster without an explicit confirmation naming the
context:

```
----------------------------------------------------------------
  about to: bring up the kmx runtime (kind, Ollama, kagent, agents)
  context:  kind-kaimahi-p1
  server:   127.0.0.1
  namespace(s): kagent, kaimahi, ollama
  posture:  local kind
----------------------------------------------------------------
```

"Local kind" is two independent checks, because a context **name** is
cosmetic — anyone can name a production context `kind-prod`. The substantive
check is the API-server address: kind publishes its API server on loopback.
Both must agree. Anything else needs `KAIMAHI_CONFIRM=<context>` or a typed
confirmation, and a non-interactive shell with neither refuses rather than
guessing. An absent `kind-*` context is admitted as "about to be created" —
that is `kmx up` on an empty machine; an absent context by any other name is
a typo, and typos are what this exists to catch.

This is [`scripts/kube-guard.sh`](../scripts/kube-guard.sh) ported to Go,
case for case; the script stays for the scripts that still use it, and both
are tested against the same cases.

## `kmx agent create`

```bash
kmx agent create fleet-reporter \
  --description "Reports what is running in the cluster." \
  --instructions ./fleet.md \
  --tools kagent-tool-server:k8s_get_resources
```

It writes `agents/fleet-reporter.yaml` — reviewable YAML that *is* the
agent, the same file you would have written by hand — then applies it behind
the guard and waits for Ready.

| Flag | Meaning |
|---|---|
| `--model <preset>` | the ModelConfig to think with (default: the plane's governed preset if it exists on the cluster, else the keyless in-cluster one) |
| `--instructions <file>` | file whose contents become the system message |
| `--description <text>` | one-line description |
| `--tools <server>:<tool>[,<tool>…]` | MCP wiring; **the allowlist is mandatory** |
| `--namespace <ns>` | default `kagent` |
| `--out <path>` | where to write it (`-` for stdout) |
| `--no-apply` | write the manifest and stop |
| `--dry-run` | server-side dry run against the live CRDs |

### Safety properties, and why each exists

| Property | Why |
|---|---|
| **Never accepts a credential** — no flag, no environment variable, no file | The generator emits Secret *references*. A scaffolder that can take a key is a scaffolder that can leak one into a file you are about to commit. |
| **Refuses key-shaped output** | Fail closed: if anything matching a known key shape reaches the manifest — including through an instructions file — writing stops. |
| **The tool allowlist is mandatory, and validated** | `--tools server` is refused; you name the tools. Naming a server alone grants every tool it offers, today and after its next release. Names must be identifiers and are quoted on emission, so a newline cannot close the YAML sequence and append a tool nobody reviewed. |
| **Block scalars indent uniformly** | A hostile instructions file cannot dedent out of the system message and become a sibling key. Multi-line values in single-line positions are refused rather than escaped. |
| **Won't overwrite** | The manifest is written with an exclusive create, so a file you have edited is never clobbered. There is no `--force`. |
| **Blast radius is the guard** | Applying goes through the same context guard as every other mutation. |
| **Preflight on the ModelConfig** | A missing ModelConfig is admitted by the API server and then fails to reconcile in silence — the Agent exists, never goes Ready, and nothing says why. kmx checks first and prints the fix. |
| **Governed by default where a plane exists** | If the plane's governed preset is on the cluster, the new agent is metered, budgeted and ledgered from its first call. On a fresh `kmx up` cluster there is no plane, so the keyless preset is used and the ungoverned warning is printed. |

The reserved names `hello-world` and `hello-tools` are refused: they are
`kmx up`'s own agents, and scaffolding over one would replace a committed
artifact that the next `kmx up` would replace right back.

## What is NOT in milestone 1

Deliberately, per D27:

| Not here | Where it is |
|---|---|
| The governance plane — budgets, the spend ledger, credential custody | `make plane` ([spend.md](spend.md)) |
| `kmx govern` — putting an agent behind the plane | `make govern`, `make govern-tools` ([tool-governance.md](tool-governance.md)) |
| Capturing a secret of any kind | `make model-secret`, `make copilot-secret`, `make slack-secret` — key-bearing steps stay in standalone scripts |
| Approvals, Slack, inbound hooks, hosted upstreams | the Makefile ([approvals.md](approvals.md), [slack.md](slack.md), [inbound.md](inbound.md), [hosted-upstreams.md](hosted-upstreams.md)) |
| A managed cluster (AKS) | `TARGET=aks make …` ([aks.md](aks.md)) |
| The network and tool probes | `scripts/*-probe.sh` |
| Publishing — a tap, a release, a package | nowhere, by decision (D26/D27) |

`kmx up` says the plane is not deployed, in one line, at the end of a run.
It is a runtime, not a governed runtime, until you run `make plane`.
