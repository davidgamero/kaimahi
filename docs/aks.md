# Running on a managed cluster (AKS)

Assumes you know the kind path ([getting-started.md](getting-started.md)),
the governance plane ([spend.md](spend.md)) and the Copilot preset
([models.md](models.md)).

The README has named AKS as the managed target from early on. For a
while nothing had ever run there, and the tooling could not even *point*
at it: every kube context was hardcoded with a `kind-` prefix. That is
fixed. `TARGET=kind|aks` selects the environment, a private-registry
image path replaces the kind side-load, and one real governed run on a
real AKS cluster happened, and was then **deleted**. This is a doc for
reproducing that run, not a description of a maintained environment.

> **Scope, honestly.** Two verified runs on 2026-09-01, each torn down
> the same day: the first proved the governed Copilot path on a managed
> cluster; the second proved the NetworkPolicy boundary is **enforced**
> there, which the first cluster, created without a policy engine,
> could not have. AKS is *demonstrated*, not *maintained*: there is no
> standing cluster, no scheduled job re-proving it, and no Azure
> credential in CI, ever.
> CI stays on kind and keyless. What re-runs on every PR is the
> portability *logic* (the context guard's decisions and the registry
> render), not the cloud.

## What is not built

The portability work adds **no new abstraction layer**. If you go
looking for a Kustomize overlay or a Helm chart for the plane, there is
none:

| Job | What already does it | Kaimahi's net-new |
|---|---|---|
| Target a different cluster | kubectl contexts | one variable: `KUBE_CTX` became overridable |
| Build an image without a local docker push | ACR Tasks (`az acr build`) | a make target that calls it |
| Grant a cluster pull rights | `az aks update --attach-acr` | a line in `scripts/aks-up.sh` |
| Environment-specific manifests | Kustomize, Helm, envsubst | **none of them**, see below |
| Provision AKS | `az aks create` | a parameterised, tagged wrapper |
| Enforce NetworkPolicy | the cluster's CNI (Cilium, here) | a flag the wrapper always passes, and a probe that proves it took |

The one place a tool would have been the obvious reach is the
environment-dependent `imagePullPolicy`. Kustomize's `images:`
transformer only takes *static* values, so the registry name would have
had to be committed, and the registry name is precisely the identifier
this repo must not commit. Instead `scripts/plane-deploy.sh` transforms
the parsed manifest at deploy time and verifies the result before
applying it.

## The two guardrails

### 1. No Azure identifiers, ever

This repo is public. A subscription or tenant GUID fingerprints the
owner; a resource-group name, an ACR login server or a cluster FQDN names
live infrastructure and invites squatting on the registry name. So every
identifier is an operator-supplied parameter, and
`scripts/check-no-azure-ids.sh`, which CI runs on every PR, refuses
GUIDs, `*.azmk8s.io` FQDNs, and any literal `<name>.azurecr.io` that is
not built from a variable or an obvious `<placeholder>`.

Run it yourself before pasting terminal output anywhere:

```bash
bash scripts/check-no-azure-ids.sh
```

### 2. Context safety, the net that replaced the hardcoding

`KUBE_CTX := kind-...` was an accidental safety feature: a mistyped
cluster name produced *"context not found"*, never a write to somebody's
production. Making it overridable removes that, and this repo's own
[CLI-PROPOSAL](CLI-PROPOSAL.md) already names the resulting foot-gun
(*"--apply on a production context by accident"*). `make down` is now a
command that can, in principle, delete a real cluster.

So every target that **writes to a cluster** depends on `guard`
(`scripts/kube-guard.sh`), which:

- **always prints** the context, the API-server host, and the namespaces
  it is about to touch;
- lets a **local kind** cluster through with no prompt, so the kind path
  and CI are unchanged;
- **demands explicit confirmation naming the context** for anything else;
- **fails closed**: no TTY and no `KAIMAHI_CONFIRM` means nothing happens.

Three targets deliberately sit outside it, because the guard checks a
*kube context* and these do not act through one: `aks-cluster` and
`aks-down` operate on Azure (and `aks-down` has its own two gates, below),
and `plane-image` on `TARGET=aks` only runs `az acr build`. `up` does not
guard either: its first step is what creates the context; every step
after that is guarded.

"Local kind" is deliberately **two independent checks**, the context
name *and* a loopback API server, because a name proves nothing. Anyone
can call an AKS context `kind-prod`; the guard is not fooled:

```console
$ KUBE_CTX=kind-sneaky bash scripts/kube-guard.sh 'apply'
----------------------------------------------------------------
  about to: apply
  context:  kind-sneaky
  server:   example.invalid
  namespace(s): kagent, kaimahi
  posture:  REMOTE / non-kind
----------------------------------------------------------------
kube-guard: 'kind-sneaky' is not a local kind cluster and there is no TTY to ask.
  to proceed:  KAIMAHI_CONFIRM=kind-sneaky make <target>
```

Read-only targets (`chat`, `status`, `ledger`, `tool-audit`,
`approvals`, `grants`) deliberately do **not** prompt: they cannot change
a cluster, and a prompt there would train you to type past it.

Confirm non-interactively, in a script or for a whole session:

```bash
export KAIMAHI_CONFIRM=$AKS_CLUSTER
```

## Prerequisites

| Prerequisite | Why |
|---|---|
| `az` CLI, logged in (`az login`) | provisioning; the tooling assumes an already-authenticated CLI, same pattern as `gh` |
| A subscription that can create resource groups, an ACR, and an AKS cluster | `--attach-acr` also needs permission to create a role assignment |
| `kubectl`, `helm`, `make`, `python3` | as for kind |
| `python3` with **PyYAML** | AKS-only, and the one prerequisite kind does not share: `scripts/plane-deploy.sh` parses `proxy.yaml` to render the registry image and pull policy. The kind branch returns before that import, so a kind-only user never needs it. `pip install pyyaml` |
| A GitHub Copilot subscription | AKS is Copilot-only: no Ollama is deployed there |

No Docker is needed for the AKS path: `az acr build` uploads the build
context and builds **in Azure**.

## From an empty subscription to a governed chat

Everything below is parameterised. Pick your own names. The ACR name
must be globally unique and alphanumeric. `AKS_CLUSTER` defaults to
`kaimahi` if you leave it unset.

```bash
export AKS_RESOURCE_GROUP=<your-rg>        # created by the script, deleted by it
export ACR_NAME=<globally-unique-name>     # 5-50 chars, alphanumeric
export AKS_CLUSTER=kaimahi-demo            # also the kube-context name
export AKS_LOCATION=westus3                # see "What this costs"
export TARGET=aks
# export AKS_NETWORK_POLICY=cilium         # the default; azure | calico also accepted
```

### 1. Provision: resource group + private ACR + AKS

```bash
make aks-cluster
```

This creates the group **tagged** `kaimahi-ephemeral`, a **private** ACR
(Basic, admin user disabled; never a public image), and a one-node AKS
cluster on the **Free** control-plane tier **with a NetworkPolicy
engine**, then grants the cluster's kubelet identity `AcrPull` and
writes the kubeconfig context.

It refuses to build inside a resource group it did not create, so a
mistyped group name cannot quietly scatter resources through someone
else's environment.

#### The policy engine is not optional

A bare `az aks create` builds a cluster whose CNI **ignores
NetworkPolicy**. The objects apply, `kubectl` lists them, and they block
nothing, which is the "worse than none" case [egress.md](egress.md)
warns about, and it is exactly what the first AKS run here had. The
script now always passes `--network-policy`, and refuses `none` or an
empty value outright rather than treating it as the operator's choice.

| `AKS_NETWORK_POLICY` | What it is | Why |
|---|---|---|
| `cilium` (**default**) | Azure CNI Overlay powered by Cilium: eBPF dataplane, `--network-dataplane cilium --network-policy cilium` | Microsoft's recommendation for new clusters, and the engine the other two are being retired in favour of. **Verified** enforcing the plane's whole matrix (below). |
| `azure` | Azure Network Policy Manager, iptables | Accepted for clusters that need it. Retiring: end of support on Linux is 2028-09-30. Not exercised here. |
| `calico` | Azure-managed Calico | Accepted. Not exercised here. |

All three ride Azure CNI Overlay (`--network-plugin azure
--network-plugin-mode overlay`): kubenet is retiring too (2028-03-31)
and Azure NPM never supported it. The script reads the engine back from
the control plane after the create and fails if it does not match, but
that only proves the flag took. Enforcement is a property of the CNI,
which the API server cannot vouch for, so the proof is step 6's
`make netpol-verify`.

**Existing clusters are not migrated.** If the cluster already exists
on a different engine, or on none, the script refuses and says what
the cluster actually has. `az aks update --network-policy` exists for
`azure` and `calico` but reimages every node pool at once, and moving
to Cilium is a dataplane upgrade with its own prerequisites; neither
belongs behind a script whose contract is "create". The cluster is
ephemeral, so the honest fix is `make aks-down` and a fresh create.

Everything after this point acts on a **remote** context, so confirm once
for the session:

```bash
export KAIMAHI_CONFIRM=$AKS_CLUSTER
```

### 2. kagent

```bash
make kagent
```

Identical to kind: the chart, the pins, and `k8s/kagent-values.yaml` are
the same. This is the portability claim in its plainest form.

### 3. The Copilot credential, **before** the plane, not after

```bash
make plane-copilot-secret     # real Copilot token -> the kaimahi namespace only
```

> **Order matters, and this is the one thing that bit us.** The proxy
> mounts `kaimahi-copilot-token` as an **optional** Secret volume. A proxy
> pod that starts before the Secret exists comes up with an empty mount,
> and every governed Copilot call then fails closed with *"upstream
> credential unavailable"* until kubelet gets around to projecting the
> new Secret, which on the verified run took minutes, long enough to look
> like a broken deployment rather than a race. Minting first means the pod
> mounts it at start and the first chat works.
>
> kind never hits this: its governed demo path is Ollama, which needs no
> upstream credential at all. If you *do* mint after deploying, don't wait:
> `kubectl -n kaimahi rollout restart deploy/kaimahi-proxy`. Rotation of
> an already-mounted token still needs no restart, as [spend.md](spend.md)
> says.

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
`imagePullPolicy: Never`, which is correct for kind and never edited here.

### 5. The agents

```bash
make agent          # hello-world, created directly on governed-copilot
make tools-agent
make govern-tools   # the tools agent behind the enforcing MCP gateway
```

On kind the agents start on the keyless Ollama preset and are switched
later. On AKS there is no Ollama, so they are created **on the governed
Copilot preset from the start**: governance is stood up before the
agents, not bolted on after.

### 6. Prove it

```bash
make chat                                     # governed Copilot completion
make ledger                                   # the row it wrote
make budget CAP_TOKENS=1 && make chat         # fails closed
make chat AGENT=hello-tools TASK='List the configmaps in the default namespace.'
make tool-audit                               # the tool call, allowed + audited
make netpol-verify                            # the boundary is ENFORCED, not just present
```

`netpol-verify` is the step that makes the policy engine above a fact
rather than a flag. It runs the probe from [egress.md](egress.md): a
control pod that must reach everything, then an unlabeled pod in the
plane's namespace that must reach **nothing**. On `TARGET=aks` the probe
expects the proxy to reach the internet on 443, because the Copilot
allowance from step 3 is always applied there.

### 6b. Optional: the Slack loop, through a public edge

The one internet-reachable thing this repo can put on a cluster is the
inbound edge for the Slack Events hook: a Caddy pod with a Let's Encrypt
certificate on a load balancer whose public IP carries a DNS label you
choose. It is opt-in, AKS-only, and documented in
[inbound.md](inbound.md#putting-it-on-the-internet); the Slack side
(`make slack-secret`, `make slack-mcp`, `make govern-slack`) is
[slack.md](slack.md). In short:

```bash
make slack-secret SLACK_CHANNEL=C0XXXXXXXXX && make slack-mcp && make govern-slack
make inbound-credential CRED_INBOUND=inbound-slack
make inbound-secret HOOK=slack-events                  # the app's Signing Secret, stdin
make slack-approvers && make notify-slack              # who may approve from Slack; the plane's own posting credential
make inbound-expose KAIMAHI_DNS_LABEL=<unique-label>   # prints the Request URL
make exposure-scan                                     # one IP, one port: 443
```

`KAIMAHI_DNS_LABEL` becomes `<label>.<region>.cloudapp.azure.com`. It
and the public IP are Azure identifiers like the others: never commit
them, redact them from evidence (`scripts/check-no-azure-ids.sh` now
refuses both shapes). When the cluster goes, the label is free for
anyone to claim: remove the Request URL from the Slack app when you
tear down.

Or the whole journey in one command, once the exports from step 1 are set:

```bash
make up      # cluster -> kagent -> copilot secret -> plane -> govern -> agents
```

`up` runs exactly the steps above, in that order. The credential comes
**before** the plane, for the reason in step 3.

### 7. Tear it down. This is not optional

```bash
KAIMAHI_CONFIRM=$AKS_RESOURCE_GROUP make aks-down
```

> **The confirmation names the RESOURCE GROUP, not the cluster.** The
> session-wide `export KAIMAHI_CONFIRM=$AKS_CLUSTER` from step 1 satisfies
> the *context* guard, and teardown deliberately does **not** accept it:
> deleting a whole resource group is a bigger act than applying to a
> context, and a standing "yes" to one cluster is not consent to destroy
> everything around it. If you forget, it refuses and prints the exact line
> to run, which is what happened on the verified run.

Deletes the resource group and everything in it, then removes the
kubeconfig entries so a dead context cannot be targeted later. Two gates
stand in front of `az group delete`, which is recursive and irreversible:

1. **Tag proof**: the group must carry the `kaimahi-ephemeral` tag that
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
| Control plane | **Free tier** | $0, no SLA. Right for an ephemeral demo |
| Node | **1 × `Standard_B4ms`**, $0.166/hr | The live kind cluster's non-Ollama workload measures ~695m CPU of requests. A 2-vCPU AKS node has only ~1.2 CPU left after system overhead, so `B2ms` ($0.0832/hr) fits but leaves no room for a rollout surge. One scheduling stall costs more than the 8¢/hr saved. Both are `AKS_NODE_SIZE`. |
| Region | **`westus3`** | Ties the cheapest US price for this SKU (westus2 is identical; southcentralus is ~20% more) and had the most regional-vCPU headroom in the subscription used. |
| Registry | **ACR Basic** | ~$0.167/day; supports ACR Tasks, which is what `az acr build` needs |
| Load balancer | AKS default (Standard) | ~$0.025/hr; created for egress even with no `LoadBalancer` Service |
| Public IP (edge, optional) | Standard static, with a DNS label | ~$0.004/hr; only while `make inbound-expose` is up |
| Edge certificate volume (optional) | 1 GiB PVC | provisioned on the default StorageClass and billed at the smallest disk tier (E1, 4 GiB, a few cents a day); deleted with the edge |
| Disks | 32 GiB OS disk + the 1 Gi Postgres PVC | rounded up to Azure's minimum billable sizes |

A run of a few hours is **well under US$2**. The first verified run
existed for about 29 minutes (17:52–18:22 UTC) and cost roughly
**US$0.10**; the NetworkPolicy run existed for about 26 minutes
(22:39–23:05 UTC) and cost about the same. Cilium adds no line item.
The dominant risk to the bill is not the rate. It is forgetting step 7.

## What differs from kind

| | kind | AKS |
|---|---|---|
| **Model** | Ollama `qwen2.5:3b`, keyless, in-cluster | **Copilot only.** No Ollama is deployed; `make ollama` refuses rather than half-deploying it. |
| **Plane image** | `docker build` + `kind load`, `imagePullPolicy: Never` | `az acr build` into a **private** ACR, pulled via the kubelet identity's `AcrPull` |
| **Agent's initial model** | starts on the keyless preset, governed later | created **on** `governed-copilot`; governance precedes the agents |
| **Storage** | the kind default `standard` provisioner | the cluster's default StorageClass, which on AKS 1.35.7 is one literally **named `default`** (`disk.csi.azure.com`), *not* `managed-csi`, which also exists but is not marked default. The PVC deliberately sets **no** `storageClassName`, so it takes whichever class the cluster defaults to; it bound `1Gi RWO` first try. Verified, not assumed: the assumption going in was `managed-csi`. |
| **NetworkPolicy** | enforced by kindnetd (kube-network-policies), nothing to configure | enforced **only** because `aks-up.sh` provisions a policy engine (Cilium by default). A cluster created without one applies the same manifests and blocks nothing. `make netpol-verify` is the proof either way. |
| **Mutating commands** | proceed with a banner | require confirmation naming the context |
| **`make down`** | `kind delete cluster` | deletes the whole tagged resource group |
| **Slack** | demonstrated ([slack.md](slack.md)) | **deliberately not deployed.** Putting a real workspace token into a temporary cloud cluster is credential exposure for little added proof. The wiring is plain CRDs plus one `tool_upstreams` entry, nothing kind-specific, but it is not demonstrated here. |
| **Cost** | free | see above |
| **CI** | every PR | never. No Azure credential belongs in a public, fork-exposed repo. |

Two smaller carry-overs, recorded rather than hidden:

- The `ollama` entry stays in the committed upstream table on AKS,
  pointing at a Service that does not exist there. Nothing calls it (the
  agents are on `governed-copilot`), and a governed-ollama request would
  fail closed at the proxy. It is left in place because the upstream
  table is a committed, environment-independent artifact.
- Node SSH access is left at the AKS default. `--ssh-access disabled` is
  the hardening step; it is not taken here because the cluster is
  short-lived and the flag's availability varies by CLI version. Worth
  taking for anything longer-lived.

## Working two clusters at once: move the local ports

These tools port-forward to fixed loopback ports: `make chat` and
`make slack-post` use `8083` (`CHAT_PORT`), `plane-admin.sh` uses `19091`
(`ADMIN_PORT`), and each probe has its own `GATEWAY_PORT` default:
`tool-denial-probe.sh` `18081`, `tool-call-probe.sh` `18082`,
`tool-admit-probe.sh` `18083`. Running a kind and an AKS verification
concurrently makes the second bind lose, and its requests land on the
*other* cluster's forward. Override per cluster:

```bash
CHAT_PORT=8183 make chat                            # chat / slack-post
ADMIN_PORT=19291 make approvals                     # plane-admin targets
GATEWAY_PORT=18281 bash scripts/tool-denial-probe.sh k8s_get_events
```

`ADMIN_PORT` is what the plane-admin targets read; `GATEWAY_PORT` is
read only by the probe scripts, which are run directly rather than
through a target; `CHAT_PORT` covers the agent-invoking targets.

**The two collisions behave differently, and one used to be silent.** An
`ADMIN_PORT` clash fails closed with a flat `HTTP 401 unauthorized` (the
other cluster's admin token does not match): safe, though the message
does not name the cause. A `CHAT_PORT` clash had no such protection: the
kagent controller on that forward is unauthenticated, so the task quietly
ran on the wrong cluster and returned a plausible reply. `make chat` now
waits for its own forward and **refuses** if it did not come up, naming
the port. `--context` cannot help here; the aiming happens at the
socket, not at kubectl.

## What was verified, and what was not

Verified live on a real AKS cluster on 2026-09-01 (Kubernetes 1.35.7,
1 × `Standard_B4ms`, westus3; evidence in the PR that shipped it, with
Azure identifiers redacted):

- the proxy image built by `az acr build` **in Azure** and pulled from the
  private ACR, with `imagePullPolicy: IfNotPresent` rendered at deploy time
  while the committed manifest still says `Never`;
- the Postgres PVC binding `1Gi RWO` on the cluster's default StorageClass;
- a governed **Copilot** chat completing, and its ledger row:
  `hello-world copilot gpt-5-mini 335 357 0 unpriced 200`;
- a budget denial failing closed: `CAP_TOKENS=1`, the task does **not**
  complete, three `denied 429` rows ledgered, month-to-date unchanged;
- a real tool call through the enforcing MCP gateway
  (`k8s_get_resources allowed 200`), proven with the probe-ConfigMap
  pattern from [tools.md](tools.md) so the answer can only come from a
  live invocation;
- custody intact: the agent-side Secret matches `^kmh_[0-9a-f]{64}$` while
  the real Copilot token stays in the `kaimahi` namespace;
- teardown: resource group deleted, and re-checked gone. 0 clusters, 0
  registries, 0 kubeconfig contexts left.

Verified live on a second AKS cluster the same day (Kubernetes 1.35.7,
Azure CNI Overlay, **Cilium 1.18** as dataplane and policy engine, 1 ×
`Standard_B4ms`, westus3; the full redacted probe output is in the PR
that shipped it):

- `aks-up.sh` provisioning with `--network-policy cilium` and reading
  the engine back from the control plane, then, on a re-run, taking the
  existing-cluster path and accepting the cluster because its engine
  matched;
- the whole `make up` journey on that cluster: kagent, the Copilot
  Secret, the plane from the private ACR, governance, both agents;
- `TARGET=aks make netpol-verify`: **boundary enforced as written**. The
  unlabeled pod in the plane's namespace, which is the enforcement check
  itself, was blocked on DNS, Postgres, 443 and 80; the proxy-shaped
  pod reached DNS, Postgres and 443 (the Copilot allowance) and was
  blocked on 80; the Slack-shaped pod reached DNS and 443 only; the
  real Postgres pod reached its own loopback and nothing else. The
  ollama column is skipped on this target, with a note, because no
  ollama Service exists there;
- a governed Copilot chat completing **through** that boundary
  afterwards, and its ledger row (`hello-world copilot gpt-5-mini 335
  239 0 unpriced 200`);
- teardown, re-checked gone.

Verified live on a third AKS cluster on 2026-09-02 (Kubernetes 1.35.7,
Cilium 1.18, 1 × `Standard_B4ms`, westus3, about three hours, roughly
US$0.70), the Slack loop through the public edge
([inbound.md](inbound.md#slack-events-the-loop)):

- `make inbound-expose`: a Let's Encrypt certificate by TLS-ALPN-01 on a
  DNS-labelled public IP; `make exposure-scan`: exactly one open port
  (443) on one public IP, none on the cluster's other public IP, one
  LoadBalancer Service cluster-wide;
- Slack's challenge answered; a real `app_mention` refused 403 and
  filed; approved bounded; the next mention admitted, the agent's reply
  posted in the thread through the gateway under a tool grant; every
  step in the inbound audit, the ledger, the tool audit and the approval
  audit;
- `make netpol-verify` with the edge's policies present: boundary
  enforced as written;
- the Slack app un-pointed (Request URL and subscription removed) before
  the edge and the resource group were deleted, re-checked gone.


The multi-node caveat in [egress.md](egress.md) was not exercised: all
runs were single-node, which is the script's default.

Also verified: `aks-down` **refuses** a resource group that lacks the tag
`aks-up.sh` sets, even when given a correct confirmation. Tested against a
throwaway untagged group, which survived.

**Not** verified on AKS: Ollama (deliberate), the `azure` and `calico`
policy engines (accepted by the script, never run), certificate renewal
(the edge lived hours; Let's Encrypt renews at day 60), and anything
about durability, upgrades, node replacement or multi-node scheduling.
Each cluster existed for well under a day and was deleted.

## Limitations

The full governed-vs-ungoverned table is in
[README.md](README.md#what-is-governed-today-and-what-is-not). Specific
to this path:

- **Demonstrated, not maintained.** Nothing re-proves the cloud
  run; only the portability logic runs in CI.
- **Copilot only.** No keyless model on AKS, so no free tier there.
- **Slack on AKS is the inbound-loop demo only**, on a cluster deleted
  the same day; the workspace token is not meant to live in a cloud
  cluster longer than that.
- **The edge is the only public surface, and it is opt-in.** Nothing
  in `make up` or `make plane` creates a LoadBalancer; `make
  exposure-scan` is how you check that stayed true.
- **The AKS cluster is not hardened** beyond a private registry, a
  tagged, ephemeral resource group, and the plane's NetworkPolicy
  boundary: default node SSH access, no durability story, and the
  `kagent` and `ollama` namespaces are as unpoliced as on kind. It is a
  demo that should be torn down the same day.
- **Only Cilium is verified.** `azure` and `calico` are accepted by the
  script because the flag is the same shape, but no run here has proven
  either enforces the matrix. Run `make netpol-verify` before trusting
  one.
