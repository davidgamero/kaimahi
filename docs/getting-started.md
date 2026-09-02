# Getting started

From an empty machine to a conversation with an agent that is defined
entirely in YAML. Everything runs on [kagent](https://kagent.dev):
kagent's controller provisions the agent, kagent's CLI talks to it.
Kaimahi adds no runtime code at this point, only the YAML, a values file,
and a Makefile that glues `kind`, `helm`, `kubectl` and the kagent CLI
together.

Every command here was run against a live cluster before it was written
down. Model replies vary run to run; where one is quoted, expect the same
substance and different wording.

## Prerequisites

| Tool | Why | Install |
|------|-----|---------|
| Docker | kind runs Kubernetes in containers | <https://docs.docker.com/get-docker/> |
| kind | local Kubernetes cluster | <https://kind.sigs.k8s.io/docs/user/quick-start/#installation> |
| kubectl | cluster interaction | <https://kubernetes.io/docs/tasks/tools/> |
| Helm | installs kagent | <https://helm.sh/docs/intro/install/> |
| make, curl | glue | your package manager |

No API key is needed anywhere: the default model is an in-cluster
[Ollama](https://ollama.com) server running `qwen2.5:3b` (free, local,
keyless). Hosted models are in [models.md](models.md).

## Up, and a first conversation

```bash
make up     # kind cluster + Ollama + model pull + kagent + two agents (first run ~5-10 min)
make chat   # ask the default question
```

`make chat` prints the raw A2A task JSON. Buried in it is the reply,
from a real run:

```text
"I am the hello_world agent, designed to greet users and provide
information about myself. I am running on Kubernetes via kagent."
```

The underscore in `hello_world` is real: kagent normalizes the agent name
internally, and that is the name the model sees and repeats.

Ask your own question, or talk to the second agent, which has a tool:

```bash
make chat TASK="What are you defined in?"
make chat AGENT=hello-tools TASK="What pods are running in the ollama namespace?"
```

The tools agent is covered in [tools.md](tools.md), including why its
prose summary is less reliable than the tool call underneath it.

```bash
make status   # environment, agents, models, runtime health, and next commands
make down     # delete the kind cluster (and everything in it, ledger included)
```

`make status` is the human-oriented overview. It identifies the target and
context, shows which model and tool server each agent uses, explains kagent's
`Ready` and `Accepted` conditions, summarizes runtime health, and retains the
pod restart counts needed for troubleshooting.

Coming from the project's old name? The cluster is now `kaimahi-p1` and
the Copilot login cache moved. See
[the FAQ](FAQ.md#i-have-a-cluster-and-paths-from-the-tomte-era).

## The agent is a YAML file

There is no kaimahi runtime to install. The whole agent,
[`k8s/hello-world.yaml`](../k8s/hello-world.yaml), is a kagent `Agent`
plus the `ModelConfig` it thinks with, in one document you can read, diff,
and review:

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
      You are Kaimahi's hello-world agent, ...
```

`kubectl apply -f` it and kagent's controller provisions a pod for it.
The topology grows the same way:
[`k8s/tools-agent.yaml`](../k8s/tools-agent.yaml) is the same shape plus
a `tools:` block. No make target ever mutates the committed file.
Switching models patches the live Agent resource, not the YAML.

## What `make up` does, step by step

The `up` target on kind runs `cluster`, `ollama`, `model`, `kagent`,
`agent`, `tools-agent`, then `status`. In plain commands:

```bash
# 1. Local Kubernetes cluster (skipped if it already exists)
kind create cluster --name kaimahi-p1

# 2. In-cluster Ollama model server (namespace, deployment, service)
kubectl apply -f k8s/ollama.yaml
kubectl -n ollama rollout status deploy/ollama

# 3. Pull the model into the Ollama pod
kubectl -n ollama exec deploy/ollama -- ollama pull qwen2.5:3b

# 4. Install kagent (CRDs chart, then the app chart), pinned to v0.9.12,
#    with Ollama as the default provider and its bundled tool server
#    switched on in locked-down form
helm upgrade --install kagent-crds \
  oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --version 0.9.12 --namespace kagent --create-namespace
helm upgrade --install kagent \
  oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version 0.9.12 --namespace kagent -f k8s/kagent-values.yaml
kubectl -n kagent wait --for=condition=Ready pods --all --timeout=420s

# 5. The agents: hello-world, then hello-tools once kagent's tool server
#    is Accepted
kubectl apply -f k8s/hello-world.yaml
kubectl apply -f k8s/tools-agent.yaml
```

Every step that writes to a cluster runs a guard first. On a local kind
cluster it prints a banner and proceeds; on anything else it demands a
confirmation naming the context. See [aks.md](aks.md) for why.

Re-running `make up` is safe for a governed agent. The `agent` and
`tools-agent` steps read the live agent first and, if it is on a
non-default ModelConfig or wired through the governed tool gateway, they
re-apply the committed YAML and then restore that state, with a `NOTE:`
line saying so. `make use PRESET=ollama` and `make ungovern-tools` are
the explicit ways back. An earlier version of `make up` silently reset
the agent; if you see that symptom, the
[FAQ entry](FAQ.md#my-governed-agent-stopped-showing-up-in-the-ledger)
covers it.

Overridable: `KIND_CLUSTER`, `KAGENT_VERSION`, `MODEL`, `AGENT`, `TASK`,
and `TARGET` (`kind` by default, or `aks`).

## Talking to the agent

`make chat` downloads the pinned kagent CLI to `bin/kagent`
(checksum-verified against the release's `.sha256` file), checks that the
agent answers through its Service, port-forwards the kagent controller to
`localhost:8083` (`CHAT_PORT`), and runs:

```bash
bin/kagent invoke --agent hello-world --task "Hello! Who are you and where are you running?"
```

If the port-forward does not come up on the port it asked for, `make chat`
refuses rather than invoking, because another cluster's forward on that
port would happily answer with a plausible reply from the wrong cluster.
Use `CHAT_PORT=<free port> make chat` when two clusters are up at once.

Other ways in, all shipped by kagent:

```bash
kubectl -n kagent get agents            # CRD status (Ready / Accepted)
bin/kagent get agent                    # via the CLI (needs the port-forward)
bin/kagent dashboard                    # kagent's web UI
```

## Choices and caveats

- **Model**: `qwen2.5:3b` (~2 GB). kagent's runtime gives every agent a
  built-in `ask_user` tool; smaller models (`llama3.2:1b`/`3b`) misfire it
  with malformed arguments and the invocation fails with
  `'str' object has no attribute 'get'`. Telling the model not to use the
  tool in the system message does not stop it. Qwen 2.5 answers plainly.
  Any tool-capable Ollama model works via `make model MODEL=<tag>` plus
  the `model:` field in the two agent YAML files, but invocation-test it
  with several fresh chats before trusting it. "It's a known model" is
  not a test. Both small-model failure modes are in the
  [FAQ](FAQ.md#the-agent-errors-with-str-object-has-no-attribute-get).
- **Models are pod-local**: the Ollama pod stores models in an
  `emptyDir`, so a pod restart re-pulls (`make model`). Deliberate: no
  PVC to manage in a demo.
- **Version pin**: kagent v0.9.12 (`KAGENT_VERSION`), the latest stable
  at the time; 0.10 was still in RC. The Agent CRD's `runtime: go`
  variant does not work out of the box at this version: the chart's
  default registry (`cr.kagent.dev`) does not carry `golang-adk:0.9.12`
  (ImagePullBackOff), though the image does exist on `ghcr.io`. If you
  need the go runtime, set `controller.agentImage.registry=ghcr.io` in
  [`k8s/kagent-values.yaml`](../k8s/kagent-values.yaml). The agents here
  stay on the default python runtime. (Verified at 0.9.12 when the pin
  was chosen; not re-verified in this restructure.)
- **`make down` deletes everything**, including the governance plane's
  Postgres and its ledger, if you have deployed them. Demo-durable, not
  backup-managed.

## Where next

- A hosted model instead of the local one: [models.md](models.md).
- What the tools agent can do and how a real tool call is proven:
  [tools.md](tools.md).
- Budgets, a ledger, and keeping real API keys away from the agent:
  [spend.md](spend.md).
- Running the plane for real — two replicas, probes, metrics, backup and
  restore, and what is still not highly available:
  [operations.md](operations.md).
