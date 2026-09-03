# `kmx` — one command for creating and running agents

`kmx` is the developer journey as a single Go binary: a cluster, a local
model, kagent, an agent, a conversation — and then the governance plane, a
governed credential, and the ledger that shows what it spent. It needs no
clone and no Makefile.

It is also the only implementation of that journey. Thirty-two Makefile
targets are one-line recipes that call this binary on the kind path — the
runtime (`up`, `cluster`, `ollama`, `model`, `kagent`, `agent`,
`tools-agent`, `chat`, `status`, `down`), the plane (`plane`, `plane-image`,
`plane-secrets`, `govern`), the reads (`ledger`, `grants`, `tool-audit`,
`approval-audit`, `approvals`, `tool-allowlist`, `plane-metrics`,
`backup`), and the operator verbs (`use`, `use-ollama`, `budget`,
`approve`, `deny`, `request`, `govern-tools`, `ungovern-tools`,
`tool-allow`, `restore`) — so CI proves the code you actually run. What is
left in the Makefile is the Slack, GitHub and inbound connector families,
secret capture, AKS and the probes.

**Status: milestone 3.** Nothing is published. `kmx` is a provisional name,
like `kaimahi` itself, and is not claimed anywhere (D26/D27/D28 on the
[board](COORDINATION.md)).

**kind only.** Everything below assumes a local kind cluster. A managed
cluster is the Makefile's path — `TARGET=aks make plane`, `TARGET=aks make
govern` — because it needs a registry, a rendered manifest and a captured
key, none of which kmx does (see [aks.md](aks.md)).

## Install

```bash
go install github.com/kaimahi-agents/kaimahi/cmd/kmx@latest
```

`@latest` is the newest tagged release; `@v0.1.0` pins one. No Homebrew tap,
no npm, crates or PyPI package — the Go module proxy and its checksum
database are the whole distribution, and no namespace of ours is claimed
(see [NAMING.md](NAMING.md)).

Without a Go toolchain, each release also carries checksum-verified binaries
for linux and macOS on amd64 and arm64. The download, the version scheme and
the upgrade path — including what happens when a migration fails — are in
[releases.md](releases.md).

From a clone, `make bin/kmx` builds the same binary and every `make` target
below uses it.

| Prerequisite | Why |
|---|---|
| Go 1.26+ | to install or build kmx — and to build the plane, which kmx fetches at its own revision |
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

kmx plane                                # the governance plane: proxy + Postgres ledger
kmx govern hello-world                   # issue the credential, put the agent behind it
kmx agent chat hello-world "Who are you?"  # the same question, now metered
kmx ledger                               # what it cost

kmx status
kmx down
```

Once the plane is up, the verbs an operator reaches for:

```bash
kmx budget hello-world --tokens 200000   # the cap it spends under
kmx tools govern --tools k8s_get_resources   # the tools agent, behind the gateway
kmx approvals                            # what is waiting for a human, and the CALL each is about
kmx approve <id> --ttl 10m --uses 1      # bounded, or it is a config change
kmx backup                               # the ledger and the audit trails, to a local file
kmx metrics                              # one replica's Prometheus exposition
```

Between the two chats nothing about the agent changed except which model
preset it thinks through. That is the whole point: governance is a preset
swap plus a credential the agent cannot read past.

## Commands

| Command | What it does |
|---|---|
| `kmx ctx` | print the context kmx will act on, where that came from, and its posture |
| `kmx ctx <context>` | select that context for later commands (recorded in kmx's config directory — `~/.config/kmx/context` on Linux; set `KMX_HOME` to put it elsewhere) |
| `kmx up` | create the kind cluster, deploy Ollama, pull the pinned model, install kagent by helm, apply both agents, wait for each to be Ready, print status |
| `kmx up --step <step>` | one step only: `cluster`, `ollama`, `model`, `kagent`, `agent`, `tools-agent` |
| `kmx agent create <name>` | scaffold `agents/<name>.yaml` and apply it |
| `kmx agent chat <name> [message]` | ask an agent one question, through `kagent invoke` |
| `kmx agent chat <name> --json` | the raw A2A task instead of the readable view (piped output is always raw) |
| `kmx plane` | build the proxy image, bootstrap the plane's secrets, deploy the plane, wait for it to serve |
| `kmx plane --step <step>` | one step only: `image`, `secrets`, `deploy` |
| `kmx plane --source <path>` | build the plane from a checkout instead of fetching it (`-` forces the fetch) |
| `kmx govern [<credential>]` | issue the governed credential (default `$CRED`), apply the governed presets, switch the agent onto one. `--ttl` sets the credential's lifetime; the plane defaults one, and there is no way to ask for "never" |
| `kmx credentials` | the governed credentials and when each one expires, soonest first, with the state an operator scans: `EXPIRED`, `EXPIRING`, `ok`, or `no expiry` (the legacy class) ([identity.md](identity.md)) |
| `kmx credential renew <name> [--ttl 720h]` | extend a credential's deadline. It moves a **date**, not material: the token does not change, so no Secret is rewritten and no credential bytes travel — which is the only reason a CLI that accepts no credential material (D27) can own this verb. Rotating the token is still `kmx govern` |
| `kmx ledger [<credential>]` | the spend ledger, newest first, plus month-to-date totals. The last column is `acted for`: who the call was made for |
| `kmx grants [<credential>]` | grants, with liveness — an expired grant is not a grant |
| `kmx audit tool\|approval [<cred>]` | the enforcement points' audit trails |
| `kmx use <preset>` | switch an agent onto a preset from `k8s/models/` (`--agent`, default `hello-world`); waits until exactly one pod is on the new template |
| `kmx budget [<credential>] [--cents n\|-] [--tokens n\|-]` | replace the monthly caps. No flags **clears** both — the same as `make budget` with no `CAP_*` |
| `kmx approvals` | the requests waiting for a decision, each with the CALL it is about |
| `kmx approve <id> [--ttl 10m] [--uses 1] [--amount n]` | mint the bounded grant. At least one of `--ttl`/`--uses` is required — an unbounded grant is a config change, not an approval |
| `kmx deny <id>` | refuse a pending request |
| `kmx request <tool\|budget\|inbound> <subject>` | file one explicitly. `--args '<json>'` (tool requests only) names the CALL to pre-approve; omitting it means the **argument-less** call, never "any call" |
| `kmx tools govern` | issue the gateway credential, set the allowlist, apply the governed `RemoteMCPServer`, repoint the agent (`--tools`, `--credential`, `--agent`, `--secret`) |
| `kmx tools allow <tool,tool\|->` | replace the allowlist. `-` is the **empty** allowlist: nothing callable without a live grant |
| `kmx tools allowlist [<credential>]` | read it back, sorted |
| `kmx tools ungovern` | put the agent back on the ungoverned tool server |
| `kmx backup [<file>]` | `pg_dump` the plane's database to a local file (default `backups/kaimahi-<UTC>.sql`, mode 0600) |
| `kmx restore <file>` | **replace** the plane's database from a backup — every table dropped and recreated |
| `kmx metrics [--pod <name>]` | one proxy replica's Prometheus exposition; the replica's name goes to stderr so stdout stays machine-readable |
| `kmx status` | agents, modelconfigs and pods |
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
| `CHAT_PORT` | `8083` | local port for the controller forward |
| `ADMIN_PORT` | `19091` | local port for the plane's admin forward |
| `OPS_PORT` | `19092` | local port for a replica's metrics forward |
| `CRED` | `hello-world` | the credential `govern` issues, and the one `ledger` and `budget` read/write by default (`grants` and `audit` default to **all** credentials) |
| `CRED_TOOLS` | `hello-tools` | the credential the MCP gateway admits — what `kmx tools` acts on, and what a `tool` request is filed against |
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

## Backup, restore, and metrics

```bash
kmx backup                       # backups/kaimahi-<UTC>.sql, mode 0600
kmx restore backups/kaimahi-....sql
kmx metrics | grep '^kaimahi_'
```

`pg_dump` and `psql` run **inside** the Postgres pod, over its unix socket,
and the bytes travel through `kubectl exec`. The database password never
leaves the pod, nothing is written to disk in the cluster, and no local
Postgres client is needed.

| Property | Why |
|---|---|
| **A dump with no trailer is not a backup** | `pg_dump` writes its trailer last, so its presence is the only well-formed positive. kmx writes to `<file>.partial` and renames only once it is there; a dump that stopped half way leaves nothing behind. `restore` checks the same trailer **before** it touches the plane. |
| **The backup is 0600 from the moment it exists** | It holds credential names and token hashes (never a token), the caps, the ledger, the audit trails and the grants. Keep it as you would the database. |
| **`restore` quiesces the plane** | The proxies are scaled to zero — in-flight calls drain — the tables are replaced, and the proxies are scaled back. A proxy admitting calls during a `--clean` restore could write ledger rows the restore then discards, or decide a budget against a half-loaded ledger. Whatever happens, the proxies come back. |
| **`restore` is guarded; `backup` is not** | `restore` rewrites the ledger. `backup` is a read, like `ledger`. |
| **`metrics` reads ONE replica** | Each replica carries its own counters, so a merged view would be arithmetic kmx invented. The ops port is on no Service, so this is a port-forward to a **pod** — and only to one that is Ready and not terminating, because a draining pod stays Running and keeps its IP. |

## What is NOT in `kmx`

Deliberately — these stay in the Makefile and the scripts, because each is
entangled with capturing a credential, which kmx accepts in no form at all:

| Not here | Where it is |
|---|---|
| The Slack, GitHub and inbound connector families | [slack.md](slack.md), [hosted-upstreams.md](hosted-upstreams.md), [inbound.md](inbound.md) |
| Capturing a secret of any kind | `make model-secret`, `make copilot-secret`, `make slack-secret` — key-bearing steps stay in standalone scripts |
| A managed cluster (AKS) | `TARGET=aks make …` ([aks.md](aks.md)) |
| The network and tool probes | `scripts/*-probe.sh` |
| Publishing — a tap, a release, a package | nowhere, by decision (D26/D27) |

`kmx up` says the plane is not deployed, in one line, at the end of a run,
and names the two commands that change that.
