# P1 runbook — hello-world agent on Kubernetes

From an empty machine to a conversation with an agent that is defined
entirely in YAML (`k8s/hello-world.yaml` — the agent-as-code artifact).

Everything runs on [kagent](https://kagent.dev): kagent's controller
provisions the agent, kagent's CLI talks to it. Tomte adds no runtime code in
this phase — just the YAML, a values file, and this runbook (see the P1 PR
for the survey that justifies each file).

## Prerequisites

| Tool | Why | Install |
|------|-----|---------|
| Docker | kind runs Kubernetes in containers | <https://docs.docker.com/get-docker/> |
| kind | local Kubernetes cluster | <https://kind.sigs.k8s.io/docs/user/quick-start/#installation> |
| kubectl | cluster interaction | <https://kubernetes.io/docs/tasks/tools/> |
| Helm | installs kagent | <https://helm.sh/docs/intro/install/> |
| make, curl | glue | your package manager |

No API key is needed anywhere: the model is an in-cluster
[Ollama](https://ollama.com) server running `qwen2.5:3b` (free, local,
keyless).

## One command

```bash
make up     # cluster + ollama + model + kagent + agent (first run ~5-10 min)
make chat   # ask the default question
make chat TASK="What are you defined in?"
```

`make down` deletes the kind cluster.

## What `make up` does, step by step

```bash
# 1. Local Kubernetes cluster
kind create cluster --name tomte-p1

# 2. In-cluster Ollama model server (namespace, deployment, service)
kubectl apply -f k8s/ollama.yaml
kubectl -n ollama rollout status deploy/ollama

# 3. Pull the model into the Ollama pod
kubectl -n ollama exec deploy/ollama -- ollama pull qwen2.5:3b

# 4. Install kagent (CRDs chart, then the app chart), pinned to v0.9.12,
#    with Ollama as the default provider and sample agents disabled
helm upgrade --install kagent-crds \
  oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --version 0.9.12 --namespace kagent --create-namespace
helm upgrade --install kagent \
  oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version 0.9.12 --namespace kagent -f k8s/kagent-values.yaml
kubectl -n kagent wait --for=condition=Ready pods --all --timeout=420s

# 5. The deliverable: apply the agent-as-code YAML
kubectl apply -f k8s/hello-world.yaml
```

## Talking to the agent

`make chat` downloads the pinned kagent CLI to `bin/kagent`
(checksum-verified), port-forwards the kagent controller to
`localhost:8083`, and runs:

```bash
bin/kagent invoke --agent hello-world --task "Hello! Who are you and where are you running?"
```

Expected reply (verbatim from a real run; model output varies):

> I am your hello-world agent named "hello_world". I am running on
> Kubernetes via kagent.

(The underscored `hello_world` is not a typo: kagent's runtime normalizes
the agent name to `hello_world` internally, and that is the name the model
sees and repeats.)

Other ways in, all shipped by kagent:

```bash
kubectl -n kagent get agents            # CRD status (Ready / Accepted)
bin/kagent get agent                    # via the CLI (needs the port-forward)
bin/kagent dashboard                    # kagent's web UI
```

## Choices and caveats

- **Model**: `qwen2.5:3b` (~2 GB). kagent's runtime gives every agent a
  built-in `ask_user` tool; smaller models (`llama3.2:1b`/`3b`) misfire it
  with malformed arguments and the invocation fails. Qwen 2.5 answers
  plainly. Any tool-capable Ollama model works via
  `make model MODEL=... ` plus the `model:` field in the two YAML files.
- **Models are pod-local**: the Ollama pod stores models in an `emptyDir`,
  so a pod restart re-pulls (`make model`). Deliberate — no PVC to manage in
  a demo.
- **Version pin**: kagent v0.9.12 (latest stable; 0.10 is still in RC).
  The Agent CRD's `runtime: go` variant does not work out of the box at this
  version: the chart's default registry (`cr.kagent.dev`) does not carry
  `golang-adk:0.9.12` (ImagePullBackOff), though the image does exist on
  `ghcr.io`. If you need the go runtime, set
  `controller.agentImage.registry=ghcr.io` in `k8s/kagent-values.yaml`;
  P1 stays on the default python runtime.
