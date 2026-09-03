# `kmx` — one command for creating and running agents

`kmx` is the developer journey as a single Go binary: a cluster, a local
model, kagent, an agent, a conversation — and then the governance plane, a
governed credential, and the ledger that shows what it spent. It needs no
clone and no Makefile.

It is also the only implementation of that journey. The Makefile's `up`,
`cluster`, `ollama`, `model`, `kagent`, `agent`, `tools-agent`, `chat`,
`status`, `down`, `plane`, `plane-image`, `plane-secrets`, `govern`,
`ledger`, `grants`, `tool-audit` and `approval-audit` targets are one-line
recipes that call this binary on the kind path, so CI proves the code you
actually run. Everything else in the Makefile — budgets, approvals,
backup/restore, Slack, GitHub, inbound hooks, secret capture, AKS, the
probes — is unchanged and still make's.

**Status: milestone 2.** Nothing is published. `kmx` is a provisional name,
like `kaimahi` itself, and is not claimed anywhere (D26/D27/D28 on the
[board](COORDINATION.md)).

**kind only.** Everything below assumes a local kind cluster. A managed
cluster is the Makefile's path — `TARGET=aks make plane`, `TARGET=aks make
govern` — because it needs a registry, a rendered manifest and a captured
key, none of which kmx does (see [aks.md](aks.md)).

## Install

```bash
go install github.com/kaimahi-agents/kaimahi/cmd/kmx@main
```

That is the only install path in this milestone: no Homebrew tap, no
release, no npm or PyPI package. Pin a reviewed commit rather than a branch
if you care what you are running — `@<sha>` works the same way.

From a clone, `make bin/kmx` builds the same binary and every `make` target
below uses it.

Plain `make` is build-only and prints the resulting binary path. It never
creates or changes a cluster; provisioning requires the explicit command:

```bash
make       # build bin/kmx
make up    # build if stale, then create/update the local runtime
```

| Prerequisite | Why |
|---|---|
| Go 1.26+ | to install or build kmx — and to build the plane, which kmx fetches at its own revision |
| Docker **or** Podman | kind runs Kubernetes in containers |
| kind | the local cluster |
| kubectl | kmx shells out to it for every read and write |
| Helm | installs kagent |

kmx fetches the pinned kagent CLI itself, checksum-verified, the first time
you chat. A clone uses the same verified kmx cache unless `KAGENT=<path>` is
explicitly supplied; the Makefile's `bin/kagent` remains for legacy
action-oriented helpers such as `slack-post`.

## The journey

```bash
kmx up                                   # kind + Ollama + the model + kagent + two agents
kmx agent chat hello-world "Who are you?"

kmx plane                                # the governance plane: proxy + Postgres ledger
kmx govern hello-world                   # issue the credential, put the agent behind it
kmx agent chat hello-world "Who are you?"  # the same question, now metered
kmx ledger                               # what it cost

kmx status
kmx down
```

Between the two chats nothing about the agent changed except which model
preset it thinks through. That is the whole point: governance is a preset
swap plus a credential the agent cannot read past.

## Commands

| Command | What it does |
|---|---|
| `kmx ctx` | print the context kmx will act on, where that came from, and its posture |
| `kmx ctx <context>` | select that context for later commands (recorded in kmx's config directory — `~/.config/kmx/context` on Linux; set `KMX_HOME` to put it elsewhere) |
| `kmx up` | check all host dependencies in one pass before the guard or first use, create the kind cluster, deploy Ollama, pull the pinned model, install kagent by helm, apply both agents, wait for each to be Ready, print status |
| `kmx up --step <step>` | one step only: `cluster`, `ollama`, `model`, `kagent`, `agent`, `tools-agent` |
| `kmx agent create <name>` | scaffold `agents/<name>.yaml` and apply it |
| `kmx agent chat <name> [message]` | ask an agent one question, through `kagent invoke` |
| `kmx agent chat <name> --json` | the raw A2A task instead of the readable view (piped output is always raw) |
| `kmx agent chat --interactive <name>` | live streamed chat in one session; shows active tools, tool calls/results, and supports session history/resume |
| `kmx plane` | build the proxy image, bootstrap the plane's secrets, deploy the plane, wait for it to serve |
| `kmx plane --step <step>` | one step only: `image`, `secrets`, `deploy` |
| `kmx plane --source <path>` | build the plane from a checkout instead of fetching it (`-` forces the fetch) |
| `kmx govern [<credential>]` | issue the governed credential (default `$CRED`), apply the governed presets, switch the agent onto one |
| `kmx ledger [<credential>]` | the spend ledger, newest first, plus month-to-date totals |
| `kmx grants [<credential>]` | grants, with liveness — an expired grant is not a grant |
| `kmx audit tool\|approval [<cred>]` | the enforcement points' audit trails |
| `kmx status` | grouped context, agent/model wiring, runtime health, restarts, and next actions |
| `kmx status -o json\|yaml` | kubectl-native combined Agents, ModelConfigs, and kagent-namespace Pods for automation |
| `kmx down` | delete the kind cluster kmx created |
| `kmx version` | the pinned kagent and model versions, the plane's image tag, and the revision `kmx plane` would fetch it at |

`kmx agent chat` prints two different shapes on purpose. A terminal gets the
reply, any tools the agent called, and the token cost. A pipe gets the raw
A2A task, byte for byte — because things parse it: CI captures this output
in eight places and `scripts/verify-chat.py` asserts on `status.state`, the
`function_call` and the `function_response` payload. `--json` forces the raw
form when a terminal wants it. If the output is not a task kmx recognises —
a transport error, a usage message — it prints what `kagent` printed rather
than guessing at a shape that is not there.

Reading, updating and deleting agents are not kmx's job — kubectl and the
kagent CLI already do them, and `kmx agent list` says so and prints the
commands. Scaffolding is the only letter of CRUD with a real gap
([CLI-PROPOSAL.md](CLI-PROPOSAL.md) is the survey that established that).

## Settings

kmx reads the names this repository already uses — the Makefile's, and
`ADMIN_PORT` from `scripts/plane-admin.sh` — with the same defaults, so
`KIND_CLUSTER=mine make up` and `KIND_CLUSTER=mine kmx up` are the same run.

| Variable | Default | Meaning |
|---|---|---|
| `KIND_CLUSTER` | `kaimahi-p1` | the cluster `up` creates and `down` deletes |
| `KUBE_CTX` | `kind-$KIND_CLUSTER` | the context to act on |
| `CONTAINER_ENGINE` | `docker` | `docker` or `podman` (sets `KIND_EXPERIMENTAL_PROVIDER`) |
| `KAGENT_VERSION` | `0.9.12` | pinned kagent chart **and** CLI |
| `MODEL` | `qwen2.5:3b` | model pulled into Ollama |
| `CHAT_PORT` | automatic | local port for the controller forward; set a number for deterministic automation |
| `ADMIN_PORT` | `19091` | local port for the plane's admin forward |
| `CRED` | `hello-world` | the credential `govern` issues, and the one `ledger` reads by default (`grants` and `audit` default to **all** credentials) |
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
| **Preflight on tools** | Before apply/dry-run, the referenced RemoteMCPServer must exist, be Accepted, and currently discover every allowlisted tool. A typo cannot become an Agent that silently never reaches Ready. |
| **Governed by default where a plane exists** | If the plane's governed preset is on the cluster, the new agent is metered, budgeted and ledgered from its first call. On a fresh `kmx up` cluster there is no plane, so the keyless preset is used and the ungoverned warning is printed. |

The reserved names `hello-world` and `hello-tools` are refused: they are
`kmx up`'s own agents, and scaffolding over one would replace a committed
artifact that the next `kmx up` would replace right back.

## Interactive chat

```bash
kmx agent chat --interactive hello-tools
# from make: INTERACTIVE=1 make chat AGENT=hello-tools
```

The header names the active agent and shows its effective selected/discovered
tools, descriptions, and whether each model/tool seam is direct or
Kaimahi-governed. A governed label requires current MCP discovery plus ready
plane replicas and Service endpoints; unknown posture refuses rather than
claiming governance. Every user message is labelled `You`; each reply carries
the active agent name. Text and correlated tool call/completion events render
as kagent streams them. The returned context ID is reused for each turn.

Commands: `/session`, `/sessions`, `/history`, `/resume <id>`, `/new`, `/retry`,
`/tools off|summary|verbose`, `/exit`.
`--session <id>` resumes a known session and displays its history. If a stream
closes while a task is still working, kmx polls that exact task ID; it never
resends the tool call. A Kaimahi governance denial still requires a separate
operator approval, followed by explicit `/retry`.

Capable terminals color semantic labels without relying on color alone: YOU
(cyan), ASSISTANT (green), TOOL (magenta), GOVERNANCE (yellow), and WARNING
(red). Every payload line is indented so model/tool text cannot impersonate a
trusted label. Set `NO_COLOR=1` or use `TERM=dumb` for plain output.

Native kagent `requireApproval` pauses are rendered in the terminal and resumed
with a structured approve/reject response. Kaimahi approvals remain a separate
security boundary; chat cannot approve its own Kaimahi request.

## How the plane gets there without a clone

`kmx plane` has to produce a container image of a Go program whose source is
**not** in the binary. It cannot be: the plane is a separate Go module under
`plane/`, and `go:embed` refuses to cross a module boundary — "cannot embed
directory: in different module". The same nested module is what makes the
answer work.

1. kmx reads **its own revision** out of its build info — the pseudo-version
   a `go install` binary carries, or the VCS revision a checkout build does.
2. It runs `go install
   github.com/kaimahi-agents/kaimahi/plane/cmd/kaimahi-proxy@<that revision>`.
   The module resolves through the public Go proxy at any commit on `main`,
   and Go's checksum database verifies what comes back.
3. It packages that binary onto the same distroless base `plane/Dockerfile`
   uses, and side-loads the image into the kind cluster with `kind load`.
4. The manifests come out of the kmx binary and are applied **as committed**.

Nothing is published to do this: no registry, no release, no tap. The plane
image never leaves your machine.

A checkout always wins. Inside a clone — or with `--source <path>` — kmx
builds `plane/Dockerfile` from the working tree instead, which is what the
Makefile passes (`--source .`) and what keeps CI proving the code a pull
request changes rather than whatever the proxy last published. `--source -`
forces the fetch even inside a checkout.

Which revision is actually running is not inferred from the image tag (the
tag is fixed, because the manifest is applied unrendered). Ask the plane:

```bash
make plane-metrics | grep kaimahi_build_info
```

### If `go install` says it cannot cross-compile

```
go: cannot install cross-compiled binaries when GOBIN is set
```

The plane's binary is built for **Linux** (that is what the kind node runs),
so on macOS every plane build is a cross-compile — and mise, asdf and
`go env -w GOBIN=…` all set `GOBIN`. kmx removes `GOBIN` from the
environment it hands the toolchain, which covers the shell-set case. A
`GOBIN` in Go's own environment file survives that, and kmx says so and
names the fix:

```bash
go env -u GOBIN         # clear it, or
kmx plane --source .    # build the plane from a checkout instead
```

## Governing an agent

```bash
kmx plane
kmx govern hello-world
```

`govern` mints an opaque Kaimahi token, stores it as the agent-side Secret
`kaimahi-governed-token`, applies the governed model presets, and switches
the agent onto one. The agent never sees a real upstream key — the plane
keeps those, and stores only the hash of the token it issued.

| Property | Why |
|---|---|
| **Cluster credentials gate it before the admin token does** | The plane's admin port is on no Service. Reaching it takes a `kubectl port-forward` to the pod, so you must already be able to reach the cluster; the admin bearer is the second lock, not the first. |
| **Tokens travel only through pipes** | The admin bearer and the issued token exist in kmx's memory and in the cluster. Neither reaches a file, an argument list, an environment listing or a log — the Secret is rendered in memory and piped into `kubectl apply -f -`. |
| **Only a genuine `NotFound` skips the switch** | An unreachable API server, an expired credential, an RBAC denial and a wrong context all look like "the agent isn't there" if you do not look. Treating them as absence prints a reassuring note, exits 0, and leaves an agent spending **outside** the plane. Anything but a real NotFound aborts. |
| **An already-issued credential is reconciled, never overwritten** | The token is shown exactly once and cannot be recovered. If the Secret is bound to a different credential kmx refuses; if it is missing, kmx tells you how to clear the row and re-issue. |
| **The switch waits for the pods, not the object** | `rollout status` returns while the old pod is still draining, and a question that lands on it gets a plausible answer from the **old** preset. kmx waits until exactly one pod is on the new template. |

Then:

```bash
kmx agent chat hello-world "Who are you?"
kmx ledger
```

## What is NOT in `kmx` yet

Deliberately, per D28(3) — these stay in the Makefile:

| Not here | Where it is |
|---|---|
| Budgets (`make budget`) and approvals (`make approvals`, `approve`, `deny`, `request`) | [approvals.md](approvals.md), [spend.md](spend.md) |
| Backup and restore, plane metrics | [operations.md](operations.md) |
| The tool gateway's wiring (`make govern-tools`, `tool-allow`) | [tool-governance.md](tool-governance.md) |
| The Slack, GitHub and inbound connector families | [slack.md](slack.md), [hosted-upstreams.md](hosted-upstreams.md), [inbound.md](inbound.md) |
| Capturing a secret of any kind | `make model-secret`, `make copilot-secret`, `make slack-secret` — key-bearing steps stay in standalone scripts |
| A managed cluster (AKS) | `TARGET=aks make …` ([aks.md](aks.md)) |
| The network and tool probes | `scripts/*-probe.sh` |
| Publishing — a tap, a release, a package | nowhere, by decision (D26/D27) |

`kmx up` says the plane is not deployed, in one line, at the end of a run,
and names the two commands that change that.
