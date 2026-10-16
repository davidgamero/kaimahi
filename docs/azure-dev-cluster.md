# Local AgentSuite → AKS 1.37

**Author locally → evaluate on AKS → package locally → lift to AKS → get an answer.**
This native Orka flow needs no GPU or container registry. Evaluation uses a
temporary candidate on AKS; it is not a local-kind-to-AKS promotion demo.

## Before you start

Use Bash, Azure CLI (`az login`), kubectl, Helm 3 and a KMX development build
with the current [workspace lifecycle](suite-workspace.md). Run these steps from
the repository root. You need Azure provisioning permissions, Kubernetes
installation/evaluation permissions, outbound image access and free local port
19281. Keep the private directory outside your checkout.

## 1. Create the AKS destination

Choose unused names. One D4s v5 node supplies 4 vCPU / 16 GiB; quota and regional
capacity must permit it. AKS Free tier still incurs compute/storage/network costs.

```sh
export SUBSCRIPTION='<subscription-id>'
export REGION=westus2 RG=kmx-dev-example CLUSTER=kmx-dev-example
export PRIVATE="$HOME/.kmx-dev-example"
mkdir -m 700 "$PRIVATE"
export KUBECONFIG="$PRIVATE/kubeconfig" KMX_HOME="$PRIVATE/state"

az account set --subscription "$SUBSCRIPTION"
az account show --query '{name:name,id:id}'
az aks get-versions --location "$REGION" -o table
az vm list-usage --location "$REGION" -o table
az group create --name "$RG" --location "$REGION" --tags purpose=kmx-dev
az aks create --resource-group "$RG" --name "$CLUSTER" --location "$REGION" \
  --kubernetes-version 1.37.0 --tier free --node-count 1 \
  --node-vm-size Standard_D4s_v5 --node-osdisk-size 64 \
  --node-osdisk-type Managed --os-sku Ubuntu --enable-managed-identity \
  --network-plugin azure --network-plugin-mode overlay --no-ssh-key
az aks get-credentials --resource-group "$RG" --name "$CLUSTER" --file "$KUBECONFIG"
kubectl --context "$CLUSTER" get nodes -o wide
```

For an existing owned cluster, use its kubeconfig and check
`az aks get-upgrades -g "$RG" -n "$CLUSTER"` before an upgrade to 1.37.0.
Do not recreate it. `--no-ssh-key` avoids generating keys; it does not disable SSH.

## 2. Install Orka and the CPU test model

```sh
export KAIMAHI_CONFIRM="$CLUSTER" # Consent for this remote test destination.
kmx --context "$CLUSTER" orka install --provider -
kubectl --context "$CLUSTER" -n orka-system create configmap native-suite-aikit \
  --from-file=config.yaml=scripts/ci/eval-loop-model.yaml
kubectl --context "$CLUSTER" create -f scripts/ci/native-suite-model.yaml
kubectl --context "$CLUSTER" -n orka-system rollout status \
  deployment/native-suite-aikit --timeout=600s
```

Orka installation provisions result-reader RBAC and persistent volumes.
`--provider -` skips the local Ollama assumption. The digest-pinned
[Qwen 3.5 2B fixture](../scripts/ci/native-suite-model.yaml) uses CPU with a
3-GiB memory request / 4-GiB limit. It is optional test inference, not a prerequisite
for developing KMX; you can instead configure an existing compatible endpoint.

Create the model binding. The placeholder below is only for this keyless fixture:

```sh
kubectl --context "$CLUSTER" create -f - <<'YAML'
apiVersion: v1
kind: Secret
metadata: {name: dev-inference-key, namespace: orka-system}
stringData: {api-key: keyless-development-placeholder}
---
apiVersion: core.orka.ai/v1alpha1
kind: Provider
metadata: {name: dev-inference, namespace: orka-system}
spec:
  type: openai
  defaultModel: qwen-3.5-2b
  baseURL: http://native-suite-aikit.orka-system.svc.cluster.local:8080/v1
  secretRef: {name: dev-inference-key, key: api-key}
YAML
kubectl --context "$CLUSTER" -n orka-system wait \
  provider/dev-inference --for=jsonpath='{.status.ready}'=true --timeout=120s
```

For real inference, provision credentials separately and use Secret references.
The model endpoint must be reachable from the cluster. Foundry inference and AKS
infrastructure are separate choices; see [models](models.md).

## 3. Author locally and evaluate on AKS

```sh
printf '%s\n' 'Answer briefly and accurately. Return the numeric result for arithmetic.' \
  > "$PRIVATE/instructions.md"
kmx create dev-greeter --workspace "$PRIVATE/workspace" \
  --model qwen-3.5-2b --instructions "$PRIVATE/instructions.md"
# Review/edit workspace/eval/example.yaml; it asks 2+2 and expects 4.
kmx eval --workspace "$PRIVATE/workspace" --to-context "$CLUSTER" \
  --inference provider:dev-inference --result-port 19281 \
  --out "$PRIVATE/passing.json"
```

Continue only after a pass. KMX cleans up the run-scoped evaluation candidate
before registering passing evidence. Unknown/failure produces no qualifying pass.

## 4. Publish the exact package, review, then lift

```sh
kmx push --workspace "$PRIVATE/workspace" --evaluation "$PRIVATE/passing.json" \
  --to-layout "$PRIVATE/release" dev-greeter:v1
# Paste the full sha256:… manifest digest printed by push.
export DIGEST='sha256:<full-manifest-digest>'
kmx lift "dev-greeter@$DIGEST" --from-layout "$PRIVATE/release" \
  --to-context "$CLUSTER" --inference provider:dev-inference \
  --evidence "$PRIVATE/passing.json" --promote --plan
kmx lift "dev-greeter@$DIGEST" --from-layout "$PRIVATE/release" \
  --to-context "$CLUSTER" --inference provider:dev-inference \
  --evidence "$PRIVATE/passing.json" --promote
```

`--promote` explicitly moves from the evaluated temporary candidate to the final
member, even on the same cluster. Final names must be absent. Digest lift reads
the package and evidence, not the source workspace; it neither builds nor evaluates.
The local OCI layout avoids an ACR requirement. For saved `dev/prod` destinations
and registry publication, see [named destinations](suite-workspace.md#named-destinations).

## 5. Ask the lifted agent a question

Lift readiness is not an answer. Submit a separate native Task:

```sh
kubectl --context "$CLUSTER" create -f - <<'YAML'
apiVersion: core.orka.ai/v1alpha1
kind: Task
metadata: {generateName: dev-greeter-answer-, namespace: orka-system}
spec:
  type: ai
  prompt: What is the capital of France?
  agentRef: {name: dev-greeter, namespace: orka-system}
  resources: {}
YAML
# Paste the generated Task name printed above.
kmx --context "$CLUSTER" task result '<task-name>' \
  --namespace orka-system --wait 5m --result-port 19281
```

Expect a completed Task and an answer such as `Paris`. This fresh answer is
separate from the candidate's evaluation evidence. The direct Task step avoids
assuming that legacy live-Agent chat is a suite-placement-aware invocation path.

## Tested scope and troubleshooting

- **2026-10-15:** fresh AKS 1.36.4 creation, followed by upgrade to **1.37.0**;
  one D4s v5, 64-GiB managed disk, pinned Orka v0.2.0 and CPU fixture. KMX
  `2238a93` passed fresh-member evaluation, cleanup, local publication and digest
  lift on 1.37. The separate `Paris` Task was exercised before the upgrade.
  Creating directly at 1.37 uses the offered version but was not the tested
  provisioning sequence. Cold model pull took about 2.5 minutes; warmup about 5s.
- **Result port occupied:** our initial evaluations returned unknown because
  another process owned port 19180. A free `--result-port 19281` resolved it.
- **Pending pods:** inspect events and PVCs before increasing node size.
  Orka added 1-GiB and 2-GiB Standard SSD claims; working CSI storage is required.
- **Windows/WSL:** run tools in the same environment; check kubeconfig and local
  forwarding. This recipe was exercised from Linux, not separately on Windows.
- **AgentSessions:** pinned local-host evaluation using this AKS model passed;
  upstream v0.1.2 also preserved an echo journal across a pod restart on 1.37.
  Neither test establishes KMX lift to AgentSessions. Use the separate
  [development-host flow](suite-workspace.md#agentsessions-development-evaluation).
  TUI, judge, image-backed and cross-cluster flows were not qualified here.

## Cost and shutdown

West US 2 retail prices checked **2026-10-15**: D4s v5 Linux **$0.192/hour**
(about $1.54/8h or $140.16/730h), plus a 64-GiB P6 OS disk at about **$9.28/month**,
runtime disks and applicable mount/transaction, load-balancer, public-IP and
egress charges. Free tier means no paid control-plane tier, not a free cluster.
No GPU, ACR or paid monitoring add-on was requested. Check current
[pricing](https://azure.microsoft.com/pricing/calculator/) for your subscription.

Stop when idle; start before the next session:

```sh
az aks stop --resource-group "$RG" --name "$CLUSTER"
# Later:
az aks start --resource-group "$RG" --name "$CLUSTER"
```

For permanent cleanup instead, inspect ownership and delete only the dedicated group:

```sh
az resource list --resource-group "$RG" -o table
az aks show -g "$RG" -n "$CLUSTER" --query nodeResourceGroup -o tsv
az group delete --name "$RG"
```

Stop deallocates compute; disks/network resources may still cost money. Observe
[AKS stop/start limits](https://learn.microsoft.com/azure/aks/start-stop-cluster).
After deletion, check the managed node group/disks are gone. Keep private evidence
and kubeconfig out of Git; preserve runtime keys/volumes together if keeping
history. See [Orka storage](orka.md#preserve-the-charts-generated-snapshot-key).
