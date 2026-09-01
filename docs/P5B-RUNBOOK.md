# P5b runbook — the governance plane on a managed cluster (AKS)

The README has named AKS as the managed target since D6. Nothing had ever
run there. Worse, the tooling could not even *point* at it:

```make
KUBE_CTX := kind-$(KIND_CLUSTER)     # every context, prefixed with kind-
```

P5b closes that gap: `TARGET=kind|aks`, a private-registry image path,
and one real governed run on a real AKS cluster — which was then
**deleted**. This is a runbook for reproducing that run, not a
description of a maintained environment.

> **Scope, honestly.** One verified run on 2026-09-01, then torn down.
> AKS is *demonstrated*, not *maintained*: there is no standing cluster,
> no scheduled job re-proving it, and no Azure credential in CI — ever.
> CI stays on kind and keyless. What re-runs on every PR is the
> portability *logic* (the context guard's decisions and the registry
> render), not the cloud.

## Survey first: what we did not build

Prime directive. The portability work adds **no new abstraction layer**:

| Job | What already does it | Kaimahi's net-new |
|---|---|---|
| Target a different cluster | kubectl contexts | one variable: `KUBE_CTX` became `?=` |
| Build an image without a local docker push | ACR Tasks (`az acr build`) | a make target that calls it |
| Grant a cluster pull rights | `az aks update --attach-acr` | a line in `scripts/aks-up.sh` |
| Environment-specific manifests | Kustomize, Helm, envsubst | **none of them** — see below |
| Provision AKS | `az aks create` | a parameterised, tagged wrapper |

The one place a tool would have been the obvious reach is the
environment-dependent `imagePullPolicy`. Kustomize was rejected: its
`images:` transformer only takes *static* values, so the registry name
would have had to be committed — and the registry name is precisely the
identifier this lane must not commit. Generating an overlay into a temp
directory to work around that is more machinery than the 40-line render
in `scripts/plane-deploy.sh`, which transforms the parsed document and
verifies the result before applying it.

## The two guardrails

### 1. No Azure identifiers, ever

This repo is public. A subscription or tenant GUID fingerprints the
owner; a resource-group name, an ACR login server or a cluster FQDN names
live infrastructure and invites squatting on the registry name. So every
identifier is an operator-supplied parameter, and
`scripts/check-no-azure-ids.sh` — which CI runs on every PR — refuses
GUIDs, `*.azmk8s.io` FQDNs, and any literal `<name>.azurecr.io` that is
not built from a variable or an obvious `<placeholder>`.

Run it yourself before pasting terminal output anywhere:

```bash
bash scripts/check-no-azure-ids.sh
```

### 2. Context safety — the net that replaced the hardcoding

`KUBE_CTX := kind-...` was an accidental safety feature: a mistyped
cluster name produced *"context not found"*, never a write to somebody's
production. Making it overridable removes that, and this repo's own
[CLI-PROPOSAL](CLI-PROPOSAL.md) already names the resulting foot-gun
(*"--apply on a production context by accident"*). `make down` is now a
command that can, in principle, delete a real cluster.

So every **mutating** target depends on `guard`
(`scripts/kube-guard.sh`), which:

- **always prints** the context, the API-server host, and the namespaces
  it is about to touch;
- lets a **local kind** cluster through with no prompt — so the kind path
  and CI are unchanged;
- **demands explicit confirmation naming the context** for anything else;
- **fails closed**: no TTY and no `KAIMAHI_CONFIRM` means nothing happens.

"Local kind" is deliberately **two independent checks** — the context
name *and* a loopback API server — because a name proves nothing. Anyone
can call an AKS context `kind-prod`; the guard is not fooled:

```console
$ KUBE_CTX=kind-sneaky bash scripts/kube-guard.sh 'apply'
  about to: apply
  context:  kind-sneaky
  server:   example.invalid
  posture:  REMOTE / non-kind
kube-guard: 'kind-sneaky' is not a local kind cluster and there is no TTY to ask.
  to proceed:  KAIMAHI_CONFIRM=kind-sneaky make <target>
```

Read-only targets (`chat`, `status`, `ledger`, `tool-audit`,
`approvals`, `grants`) deliberately do **not** prompt: they cannot change
a cluster, and a prompt there would train you to type past it.

Confirm non-interactively — in a script, or for a whole session:

```bash
export KAIMAHI_CONFIRM=$AKS_CLUSTER
```

## Prerequisites

| Prerequisite | Why |
|---|---|
| `az` CLI, logged in (`az login`) | provisioning; the lane assumes an already-authenticated CLI, same pattern as `gh` |
| A subscription that can create resource groups, an ACR, and an AKS cluster | `--attach-acr` also needs permission to create a role assignment |
| `kubectl`, `helm`, `make`, `python3` | as for kind |
| A GitHub Copilot subscription | AKS is Copilot-only (D15) |

No Docker is needed for the AKS path: `az acr build` uploads the build
context and builds **in Azure**.

## From an empty subscription to a governed chat

Everything below is parameterised. Pick your own names — the ACR name
must be globally unique and alphanumeric.

```bash
export AKS_RESOURCE_GROUP=<your-rg>        # created by the script, deleted by it
export ACR_NAME=<globally-unique-name>     # 5-50 chars, alphanumeric
export AKS_CLUSTER=kaimahi-p5b             # also the kube-context name
export AKS_LOCATION=westus3                # see "What this costs"
export TARGET=aks
```

### 1. Provision: resource group + private ACR + AKS

```bash
make aks-cluster
```

This creates the group **tagged** `kaimahi-ephemeral=p5b`, a **private**
ACR (Basic, admin user disabled — D15: not a public image), and a
one-node AKS cluster on the **Free** control-plane tier, then grants the
cluster's kubelet identity `AcrPull` and writes the kubeconfig context.

It refuses to build inside a resource group it did not create, so a
mistyped group name cannot quietly scatter resources through someone
else's environment.

Everything after this point acts on a **remote** context, so confirm once
for the session:

```bash
export KAIMAHI_CONFIRM=$AKS_CLUSTER
```

### 2. kagent

```bash
make kagent
```

Identical to kind — the chart, the pins, and `k8s/kagent-values.yaml` are
the same. This is the portability claim in its plainest form.

### 3. The Copilot credential — **before** the plane, not after

```bash
make plane-copilot-secret     # real Copilot token -> the kaimahi namespace only
```

> **Order matters, and this is the one thing that bit us.** The proxy
> mounts `kaimahi-copilot-token` as an **optional** Secret volume. A proxy
> pod that starts before the Secret exists comes up with an empty mount,
> and every governed Copilot call then fails closed with *"upstream
> credential unavailable"* until kubelet gets around to projecting the
> new Secret — which on the verified run took minutes, long enough to look
> like a broken deployment rather than a race. Minting first means the pod
> mounts it at start and the first chat works.
>
> kind never hits this: its governed demo path is Ollama, which needs no
> upstream credential at all. If you *do* mint after deploying, don't wait
> — `kubectl -n kaimahi rollout restart deploy/kaimahi-proxy`. Rotation of
> an already-mounted token still needs no restart, as P4a documents.

Custody is unchanged: the **real** Copilot token exists only as a Secret
mounted into the proxy pod, in the `kaimahi` namespace. The agent gets an
opaque `kmh_` token and never holds a provider key.

### 4. The governance plane, from the private registry

```bash
make plane
make govern                   # issue the agent's opaque kmh_ token; apply presets
```

`plane-image` runs `az acr build` (built in Azure; no local docker build,
no `docker push`, no registry login on your machine), and
`scripts/plane-deploy.sh` renders `k8s/plane/proxy.yaml` with the ACR
image reference and a real pull policy. The committed manifest keeps
`imagePullPolicy: Never` — correct for kind, and never edited here.

### 5. The agents

```bash
make agent          # hello-world, created directly on governed-copilot
make tools-agent
make govern-tools   # the tools agent behind the enforcing MCP gateway
```

On kind the agents start on the keyless Ollama preset and are switched
later. On AKS there is no Ollama (D15), so they are created **on the
governed Copilot preset from the start** — governance is stood up before
the agents, not bolted on after.

### 6. Prove it

```bash
make chat                                     # governed Copilot completion
make ledger                                   # the row it wrote
make budget CAP_TOKENS=1 && make chat         # fails closed
make chat AGENT=hello-tools TASK='List the configmaps in the default namespace.'
make tool-audit                               # the tool call, allowed + audited
```

Or the whole journey in one command, once the exports from step 1 are set:

```bash
make up      # cluster -> kagent -> copilot secret -> plane -> govern -> agents
```

`up` runs exactly the steps above, in that order. Note that the credential
comes **before** the plane, for the reason in step 3.

### 7. Tear it down — this is not optional

```bash
KAIMAHI_CONFIRM=$AKS_RESOURCE_GROUP make aks-down
```

> **Note the confirmation names the RESOURCE GROUP, not the cluster.** The
> session-wide `export KAIMAHI_CONFIRM=$AKS_CLUSTER` from step 1 satisfies
> the *context* guard, and teardown deliberately does **not** accept it:
> deleting a whole resource group is a bigger act than applying to a
> context, and a standing "yes" to one cluster is not consent to destroy
> everything around it. If you forget, it refuses and prints the exact line
> to run — which is what happened on the verified run.

Deletes the resource group and everything in it, then removes the
kubeconfig entries so a dead context cannot be targeted later. Two gates
stand in front of `az group delete`, which is recursive and irreversible:

1. **Tag proof** — the group must carry `kaimahi-ephemeral=p5b`, the tag
   `aks-up.sh` sets. A group this tooling did not create **cannot** be
   deleted by it at all. This is what makes a typo'd group name harmless
   rather than catastrophic.
2. **Explicit confirmation** naming the group.

It waits for completion and then re-checks that the group is gone, because
*"I asked Azure to delete it"* is not the same claim as *"it is gone"*.

## What this costs

Measured choices, not guesses (Azure retail prices API, 2026-09-01):

| Item | Choice | Why |
|---|---|---|
| Control plane | **Free tier** | $0, no SLA — right for an ephemeral demo |
| Node | **1 × `Standard_B4ms`**, $0.166/hr | The live kind cluster's non-Ollama workload measures ~695m CPU of requests. A 2-vCPU AKS node has only ~1.2 CPU left after system overhead, so `B2ms` ($0.0832/hr) fits but leaves no room for a rollout surge. One scheduling stall costs more than the 8¢/hr saved. Both are `AKS_NODE_SIZE`. |
| Region | **`westus3`** | Ties the cheapest US price for this SKU (westus2 is identical; southcentralus is ~20% more) and had the most regional-vCPU headroom in the subscription used. |
| Registry | **ACR Basic** | ~$0.167/day; supports ACR Tasks, which is what `az acr build` needs |
| Load balancer | AKS default (Standard) | ~$0.025/hr; created for egress even with no `LoadBalancer` Service |
| Disks | 32 GiB OS disk + the 1 Gi Postgres PVC | rounded up to Azure's minimum billable sizes |

A run of a few hours is **well under US$2**. The verified run existed for
about 29 minutes (17:52–18:22 UTC) and cost roughly **US$0.10**. The
dominant risk to the bill is not the rate — it is forgetting step 7.

## What differs from kind — the honest list

| | kind | AKS |
|---|---|---|
| **Model** | Ollama `qwen2.5:3b`, keyless, in-cluster | **Copilot only** (D15). No Ollama is deployed; `make ollama` refuses rather than half-deploying it. |
| **Plane image** | `docker build` + `kind load`, `imagePullPolicy: Never` | `az acr build` into a **private** ACR, pulled via the kubelet identity's `AcrPull` |
| **Agent's initial model** | starts on the keyless preset, governed later | created **on** `governed-copilot`; governance precedes the agents |
| **Storage** | the kind default `standard` provisioner | the cluster's default StorageClass, which on AKS 1.35.7 is one literally **named `default`** (`disk.csi.azure.com`) — *not* `managed-csi`, which also exists but is not marked default. The PVC deliberately sets **no** `storageClassName`, so it takes whichever class the cluster defaults to; it bound `1Gi RWO` first try. Verified, not assumed — the assumption going in was `managed-csi`. |
| **Mutating commands** | proceed with a banner | require confirmation naming the context |
| **`make down`** | `kind delete cluster` | deletes the whole tagged resource group |
| **Slack (P5a)** | demonstrated | **deliberately not deployed.** Putting a real workspace token into a temporary cloud cluster is credential exposure for little added proof. The wiring is plain CRDs plus one `tool_upstreams` entry — nothing about it is kind-specific — but this lane does not demonstrate it. |
| **Cost** | free | see above |
| **CI** | every PR | never. No Azure credential belongs in a public, fork-exposed repo. |

Two smaller carry-overs, recorded rather than hidden:

- The `ollama` entry stays in the committed upstream table on AKS,
  pointing at a Service that does not exist there. Nothing calls it — the
  agents are on `governed-copilot` — and a governed-ollama request would
  fail closed at the proxy. It is left in place because the upstream
  table is a committed, environment-independent artifact.
- Node SSH access is left at the AKS default. `--ssh-access disabled` is
  the hardening step; it is not taken here because the cluster is
  short-lived and the flag's availability varies by CLI version. Worth
  taking for anything longer-lived.
- **Working two clusters at once? Move the local ports.** `plane-admin.sh`
  port-forwards the admin port to `127.0.0.1:19091` and the probes use
  `18081`. Running a kind and an AKS verification concurrently makes the
  second bind lose, and its requests land on the *other* cluster's forward
  — which shows up as a flat `HTTP 401 unauthorized`, because the admin
  token does not match. It fails closed, but the message does not point at
  the cause. Override `ADMIN_PORT` and `GATEWAY_PORT` per cluster:

  ```bash
  ADMIN_PORT=19291 GATEWAY_PORT=18281 make approvals
  ```

## What was verified, and what was not

Verified live on a real AKS cluster on 2026-09-01 (Kubernetes 1.35.7,
1 × `Standard_B4ms`, westus3; evidence in the PR with Azure identifiers
redacted):

- the proxy image built by `az acr build` **in Azure** and pulled from the
  private ACR, with `imagePullPolicy: IfNotPresent` rendered at deploy time
  while the committed manifest still says `Never`;
- the Postgres PVC binding `1Gi RWO` on the cluster's default StorageClass;
- a governed **Copilot** chat completing, and its ledger row —
  `hello-world copilot gpt-5-mini 335 357 0 unpriced 200`;
- a budget denial failing closed: `CAP_TOKENS=1`, the task does **not**
  complete, three `denied 429` rows ledgered, month-to-date unchanged;
- a real tool call through the enforcing MCP gateway
  (`k8s_get_resources allowed 200`), proven with the P3 probe-ConfigMap
  pattern so the answer can only come from a live invocation;
- custody intact: the agent-side Secret matches `^kmh_[0-9a-f]{64}$` while
  the real Copilot token stays in the `kaimahi` namespace;
- teardown: resource group deleted, and re-checked gone — 0 clusters, 0
  registries, 0 kubeconfig contexts left.

Also verified: `aks-down` **refuses** a resource group that lacks the tag
`aks-up.sh` sets, even when given a correct confirmation — tested against a
throwaway untagged group, which survived.

**Not** verified on AKS: the Slack path (deliberate, above), Ollama
(deliberate), NetworkPolicy egress (unbuilt, out of scope), and anything
about durability, upgrades, node replacement or multi-node scheduling — the
cluster existed for about half an hour and was deleted.
