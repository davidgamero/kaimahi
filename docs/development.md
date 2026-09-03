# Development

For engineers working **on** Kaimahi. If you want to *use* it, start at
[README.md](README.md) and [getting-started.md](getting-started.md); this
page assumes you are changing code.

Contribution process and PR expectations live in
[../CONTRIBUTING.md](../CONTRIBUTING.md). This page is the mental model, the
build, and the traps.

## The one-paragraph model

kagent runs the agents. Kaimahi does not replace it, wrap it, or fork it —
it **sits in two seams kagent already has** and makes the traffic through
them accountable:

- an agent's `ModelConfig.baseUrl` can point anywhere, so Kaimahi points it
  at its own proxy → every model call is authenticated, budget-checked and
  ledgered;
- an agent's tools come from an MCP server URL, so Kaimahi points it at its
  own gateway → every tool call is authenticated, allowlisted and audited.

Nothing else changes. The agent is still a plain kagent `Agent` CRD, and
pointing it back at the direct URLs makes it ungoverned again. **Governance
is opt-in per agent, and that is a deliberate property, not an oversight** —
it is also the first thing to state honestly in any doc you write.

## Repository layout

| Path | What it is |
|---|---|
| `plane/` | The Go governance plane. The only compiled artifact in the repo. |
| `plane/cmd/kaimahi-proxy/` | One binary, five listeners (below). |
| `plane/internal/` | `proxy` (LLM data path + admin), `gateway` (MCP), `inbound` (webhooks), `meter` (budgets), `pricing` (tokens→cents), `store`/`db` (Postgres + migrations), `config` (upstream table), `redact` (log scrubbing), `metrics` (Prometheus, fixed label vocabularies), `ops` (metrics listener + probes). |
| `k8s/` | Everything applied to a cluster: agents, model presets, the plane, network policy, scenario fixtures. |
| `k8s/models/` | One `ModelConfig` per preset. `governed-*` point at the proxy; the rest go direct. |
| `scripts/` | Key-handling and check scripts. Anything touching a credential lives here, never in a make recipe. |
| `docs/` | User docs by capability, plus the board (`COORDINATION.md`). |
| `Makefile` | The operator interface. Every mutating target depends on `guard`. |

## Build and verify

Everything below is runnable from a clean checkout with no cluster.

```bash
# Go plane — the only thing that compiles.
# `gofmt -l` LISTS unformatted files but still exits 0, so it has to be
# wrapped to actually fail the chain — the same trap as `! grep` below.
cd plane && test -z "$(gofmt -l .)" && go vet ./... && go build ./... && go test ./...

# Repository checks (these are the CI hygiene job)
python3 scripts/check-doc-links.py
python3 scripts/check-readme-front-door.py
python3 scripts/check-readme-front-door-test.py
python3 scripts/check-brand-assets.py
# Azure identifiers, by SHAPE: GUIDs (subscription/tenant), AKS API-server
# hostnames, literal container-registry login servers, literal public
# load-balancer DNS labels, public IPv4 addresses. The self-test runs
# first. With no arguments it scans what git would commit
# (tracked + unignored files), so a transcript you are about to paste
# into a PR must be saved to a file and passed by path. A bare resource
# group or cluster NAME is just a string and is not detected: read for
# those yourself before pasting.
bash   scripts/check-no-azure-ids-test.sh && bash scripts/check-no-azure-ids.sh
bash   scripts/check-no-azure-ids.sh path/to/transcript.txt
bash   scripts/kube-guard-test.sh

# Container image
docker build -t kaimahi-proxy:dev plane/
```

The image is ~18 MB: a static binary on `distroless/static-debian12:nonroot`
— no shell, no package manager, non-root by default.

### The local loop

```bash
make up KIND_CLUSTER=<your-name>      # kind + Ollama + kagent + agents
make plane KIND_CLUSTER=<your-name>   # build, load, deploy the plane + Postgres
make govern KIND_CLUSTER=<your-name>  # issue a credential, switch the agent onto it
make chat KIND_CLUSTER=<your-name> AGENT=hello-world TASK="..."
make ledger KIND_CLUSTER=<your-name>
make down KIND_CLUSTER=<your-name>
```

Podman works too — pass `CONTAINER_ENGINE=podman` to every target for that
cluster (see
[getting-started.md](getting-started.md#using-podman-instead-of-docker)).
The Makefile derives `KIND_EXPERIMENTAL_PROVIDER` from it, and swaps the
image load to `podman save` + `kind load image-archive`, because `kind load
docker-image` cannot see podman's images.

**Always pass your own `KIND_CLUSTER`.** `kaimahi-p1` is the shared demo
cluster and lanes have collided over it before. The board records this as a
rule, not a suggestion.

A full `make up` is 5–10 minutes, most of it pulling the model. Expect a
laptop node to be CPU-bound once Ollama, kagent, Postgres and the plane are
all running: a 3B model on a saturated single node can exceed the kagent
controller's A2A timeout even though the governed calls themselves succeed.
If a chat times out, check `make ledger` before assuming the plane is broken.

## How the plane actually works

One binary (two replicas), five HTTP listeners, one Postgres:

| Port | Listener | Carries |
|---|---|---|
| 8080 | **data** | OpenAI-compatible model traffic from agents |
| 8081 | **MCP gateway** | JSON-RPC tool traffic from agents |
| 8082 | **inbound** | authenticated webhooks from outside |
| 9091 | **admin** | issuing credentials, budgets, approvals — bearer-token, cluster-internal |
| 9092 | **ops** | Prometheus `/metrics`, `/readyz`, `/livez` — no auth, on no Service ([operations.md](operations.md)) |

The admin port is deliberately separate from every data path. `make budget`,
`make approve` and friends reach it through a port-forward in
`scripts/plane-admin.sh`; it is not exposed. The ops port is on no
Service either; kubelet probes it, `make plane-metrics` port-forwards to
a pod, and a scraper gets in only through the NetworkPolicy allowance.

The process holds no governance state. Every decision that must be
exact — a budget admission, a grant use, a replay check, a filing, an
approval — is one Postgres transaction under a lock on the credential's
row (`lockCredential` in `store/`), so the replicas agree by
construction; what stays per replica (the pre-auth rate limiter, the
bounded queues, the fail-closed breakers) is listed in
[operations.md](operations.md) with the reason.

### The credential is the unit of governance

Every governed call carries a Kaimahi-issued opaque token (`kmh_…`), not a
provider key. The plane stores only its SHA-256. That one token is what a
budget is attached to, what a tool allowlist is attached to, and what shows
up in the ledger and audit rows.

The property worth protecting: **the agent never holds a real upstream
credential.** For a governed Copilot preset the agent's Secret holds a `kmh_`
token; the real Copilot token is mounted only into the proxy pod. Breaking
that is the most serious kind of regression in this repo.

### Where the durable state lives

Postgres, migrated from `plane/internal/db/migrations/`:

| Table | Holds |
|---|---|
| `credential` | issued tokens (hashed), their budgets |
| `ledger_entry` | one row per billed model call — tokens, cents, status |
| `spend_reservation` | calls admitted under a cap whose ledger row has not landed yet — the hold that makes budgets exact under concurrency; the ledger write deletes it |
| `tool_allowlist` | which tools a credential may call |
| `tool_audit` | every tool call, allowed **and denied** |
| `approval_request`, `permit_grant`, `approval_audit` | the deny → approve → bounded grant cycle |
| `inbound_audit` | append-only, and doubles as the webhook replay guard |

### What is configuration, not code

`k8s/plane/upstreams.yaml` is the upstream table: base URL plus **exactly
one** allowed forwarded path per upstream, for models and for MCP servers.

`k8s/plane/network-policy.yaml` is a **complementary** control, not the same
rule restated. They constrain different things and neither substitutes for
the other:

| | Enforces | Cannot see |
|---|---|---|
| `upstreams.yaml` | which base URL, and the one permitted path on it | anything below the process — a pod can still open sockets the table never mentions |
| `network-policy.yaml` | which pods, namespaces, IP blocks and ports are reachable at all | URLs, paths, or HTTP at all — it is L3/L4 |

So the policy stops the pod reaching a host that is not allowed; the
upstream table stops the proxy forwarding to a path that is not allowed on a
host it *can* reach. If you are adding a destination you are editing both —
and writing neither a new client nor a new egress path.

## Invariants

These are the things that get a PR sent back. Most were learned the
expensive way.

1. **Do not rebuild what kagent ships.** Agent runtime, CRDs, CLI,
   dashboard, MCP servers. Net-new components need a written survey in the
   PR justifying them. Ignoring this caused a full project restart once.
2. **Fail closed.** A verify path accepts only a well-formed positive. WAFs
   return HTML with a 200; gateway-style services return 200 with an error
   envelope for a bad key. `! grep` is not a gate — distinguish "no match"
   from "the scanner failed to run".
3. **Keys come from stdin, and go into a Secret.** Never argv, env listings,
   YAML, ConfigMaps, or logs. Key-bearing steps live in `scripts/` with
   `set -euo pipefail`, never in a make recipe — make runs recipes without
   pipefail, and a failed pipe stage can fail *open*. That exact bug once
   stored an empty Secret after a failed token exchange.
4. **Record spend before honouring a failure.** A billed call gets a ledger
   row even when the surrounding operation errors.
5. **Never infer that something is free.** Free is an explicit
   classification in the upstream table, not a guess from a URL.
6. **Mutating make targets depend on `guard`.** `scripts/kube-guard.sh`
   checks the context name *and* the API server address, because a context
   named `kind-prod` can point at production. Confirm non-interactively with
   `KAIMAHI_CONFIRM=$KUBE_CTX`.
7. **Say what is actually verified.** The repo's status vocabulary is
   continuously tested / demonstrated once / schema-valid / proposed /
   unbuilt. "It should work" is not one of them.
8. **A governance decision is a Postgres transaction under the credential
   lock, never a read-then-act in Go.** The plane runs two replicas that
   share nothing but the database; anything decided from an unlocked read
   or from process memory is a race between them. New limits get a
   concurrent test in `store_pg_test.go` (real Postgres, goroutines
   racing the real SQL), not an argument.
9. **No identifier is a metric label.** Label values come from the fixed
   vocabularies in `plane/internal/metrics` or the two public name shapes
   (credential name, upstream name); the label-set test fails on anything
   else. A token, a channel, a user, a request or a delivery id never
   becomes a series.

## Traps

- **`.gitignore` rules are unanchored by default.** `bin/` matches at every
  depth. A tracked-looking file can be silently skipped by `git add -A`,
  and you will not be told. Verify the committed tree
  (`git ls-files --error-unmatch <path>`), not the working directory.
- **macOS ships Python 3.9.** Repo scripts must not use PEP 604 (`str |
  None`) at import time without `from __future__ import annotations`, or
  they die before running a single check on a contributor's laptop while
  passing in CI.
- **`git stash -u` will take files that were ignored a moment ago.** Change
  an ignore rule, stash, and the newly-visible file goes with it.
- **A missing `ModelConfig` is admitted, then fails to reconcile.** The
  Agent reports `Accepted=False` in a status condition you only see if you
  look. Check conditions, not just `READY`.
- **`make up` does not run `govern` on kind.** A fresh cluster has no
  governed presets until you ask for them.
- **Re-running `make up` re-applies the agent** and can quietly drop it back
  onto an ungoverned preset.
- **The proxy image is distroless: there is no shell and no `cat` to
  `kubectl exec`.** A wait loop built on `exec … cat` never succeeds; it
  burns its full timeout and moves on (CI carried one for two phases).
  Wait on the plane's own behaviour, or restart the Deployment so pods
  start with a freshly created Secret already projected.
- **`kubectl port-forward svc/…` sticks to one pod.** With two replicas,
  a probe through a Service exercises whichever pod it happened to pick.
  The race and kill probes port-forward each `pod/` separately for that
  reason; do the same when the claim is "both replicas".
- **A bare `wait` in a script waits on the port-forwards too**, which
  never exit. Collect worker PIDs and `wait` on those.
- **A container engine's clusters are invisible to the other engine.** `kind
  get clusters` under docker will not list a podman cluster, so the Makefile
  cheerfully tries to create one that already exists. Keep
  `CONTAINER_ENGINE` consistent for a given `KIND_CLUSTER`.
- **Restarting the podman machine stops kind's node container.** `make
  cluster CONTAINER_ENGINE=podman` starts every node belonging to the named
  cluster and waits for both the API server and CoreDNS before returning,
  so `make up` can safely continue.
- **First `make up` on podman can fail at the ollama rollout.** Pulling the
  ~1.9GB image through the podman VM took 5m04s here, past the target's
  300s `rollout status` timeout, so make stops even though the pull
  succeeds. Re-run `make up`; it is idempotent and continues.
- **A podman machine with no volume mounts cannot read your checkout**, so
  `podman build` fails with `faccessat <path>: connection refused`. Volumes
  are fixed at `podman machine init` time — `podman machine set` has no flag
  for them — so the machine has to be recreated.

## Where to look when something is wrong

```bash
kubectl -n kagent get agents                        # Accepted / Ready
kubectl -n kagent describe agent <name>             # the real error
kubectl -n kagent logs deploy/<agent> --tail=50     # what it called
kubectl -n kaimahi logs -l app=kaimahi-proxy --prefix   # governance decisions, both replicas
make ledger    CRED=<name>                          # was the call metered?
make tool-audit CRED_TOOLS=<name>                   # was the tool allowed?
make plane-metrics                                  # one replica's counters, queue depths, breakers
```

The ledger and the audit are the source of truth for "did governance
actually happen". A passing conversation proves nothing on its own — the
rows do.
