# User guide

Everything in this guide is shipped and was run against a live cluster
before being written down. For what's *proposed* rather than built, see
[CLI-PROPOSAL.md](CLI-PROPOSAL.md) and the phase-4 sections of the
[README](../README.md).

## What this is

Kaimahi is an incubation project — an idea being worked out in the open.
The working part: an agent defined in one YAML file, running on Kubernetes
via [kagent](https://kagent.dev), driven entirely from your terminal. It
can think with a free local model or a hosted one, call a tool over MCP,
and — the part kaimahi actually adds — have its LLM spend governed: budgets
that fail closed, a ledger of every call, and real API keys the agent never
sees. The rest of the governance plane (tool governance, approvals) and the
`kaimahi create` CLI are proposals, not products.

## Zero to a working agent

You need Docker, kind, kubectl, Helm, make, and curl (the README's
[Quickstart](../README.md#quickstart) table has install links). No API key —
the default model is an in-cluster Ollama server running `qwen2.5:3b`.

```bash
make up     # kind cluster + Ollama + kagent + two agents (~5-10 min first run)
make chat   # ask the default question
```

`make chat` prints the raw A2A task JSON. Buried in it is the reply:

```text
"I am the hello_world agent, designed to greet users and provide
information about myself. I am running on Kubernetes via kagent."
```

Model replies vary run to run — you'll get different wording, same
substance. The underscore in `hello_world` is real: kagent normalizes the
agent name internally, and that's the name the model sees.

Ask your own question, or talk to the tools agent:

```bash
make chat TASK="What are you defined in?"
make chat AGENT=hello-tools TASK="What pods are running in the ollama namespace?"
```

`make status` shows agents, ModelConfigs, and pods. `make down` deletes the
cluster (and everything in it, ledger included).

## The agent is a YAML file

There is no kaimahi runtime to install. The whole agent —
[`k8s/hello-world.yaml`](../k8s/hello-world.yaml) — is a kagent `Agent`
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

`kubectl apply -f` it and kagent's controller provisions a pod for it. The
topology grows the same way: [`k8s/tools-agent.yaml`](../k8s/tools-agent.yaml)
is the same shape plus a `tools:` block. The original file is never mutated
by any make target — switching models patches the live Agent resource, not
the YAML.

## Models: presets and switching

Every model endpoint is a committed `ModelConfig` preset in
[`k8s/models/`](../k8s/models/). One command switches the agent:

```bash
make use PRESET=anthropic    # apply the preset, repoint the agent, wait Ready
make chat                    # same conversation, different brain
make use-ollama              # back to the free in-cluster model
```

The presets: `ollama`, `github-copilot`, `anthropic`, `openai`,
`openrouter`, `azure-foundry`, `openai-compatible`, and the governed pair
`governed-ollama` / `governed-copilot` (below). Only `ollama` and
`github-copilot` have been live-verified — the rest are "schema-valid
only", which means CI dry-runs them against the real CRDs but nobody has
bought a completion through them yet ([FAQ](FAQ.md#what-schema-valid-only-means)
has the honest version).

Two things bite people here:

- Create the preset's key Secret *before* switching. An agent pointed at a
  ModelConfig whose Secret is missing never becomes Ready, and `make use`
  hangs waiting for it.
- A plain hosted preset is a live credit card. Nothing meters it. Either
  accept that or use a governed preset.

## Keys and custody

Keys go into Kubernetes Secrets from stdin, and nowhere else — never argv,
YAML, env listings, or logs:

```bash
make model-secret NAME=anthropic-api-key
# Paste the API key, press Enter, then Ctrl-D:
```

GitHub Copilot is the odd one out: there's no long-lived key to paste.
`make copilot-secret` walks you through GitHub's device login in the
browser, exchanges it for a short-lived Copilot token, and stores only that
token in-cluster. It expires within hours; re-run the target to rotate
([FAQ](FAQ.md#the-copilot-preset-worked-yesterday-and-fails-today)).

Custody gets stronger once the governance plane is up. With a governed
preset, the agent's Secret holds a kaimahi-issued opaque token (`kmh_…`) —
not a real key. The real upstream credential is mounted only to the proxy
pod, and the plane's database stores only a hash of the opaque token. You
can check this yourself: `kubectl -n kagent get secret
kaimahi-governed-token -o jsonpath='{.data.api-key}' | base64 -d` starts
with `kmh_`, not `sk-` or a GitHub token.

## Governing spend

This is the incubated thesis, and its first slice runs today. `make govern`
routes the agent's LLM calls through an in-cluster proxy that
authenticates, budget-checks, forwards, and ledgers every call:

```bash
make plane       # build the proxy image, load into kind, deploy proxy + Postgres
make govern      # issue the credential, switch hello-world through the proxy
make chat        # works as before — but now metered and ledgered
make ledger      # see the rows
```

The ledger from a real run:

```text
created (UTC)       credential   upstream  model                in    out  cents source   status
2026-09-01T03:41:45 hello-world  ollama    qwen2.5:3b          371     27      0 free     200
```

`source` says why the cost is what it is: `free` is an explicit
classification of the in-cluster ollama upstream (never an inference from
a $0-looking URL), `priced` means a configured price row was applied,
`unpriced` means tokens were counted but no price exists, and `denied`
means the request never went upstream.

### Budgets

Caps are monthly (calendar month, UTC), per credential:

```bash
make budget CAP_TOKENS=100000    # token cap — the only lever for the free tier
make budget CAP_CENTS=500        # cents cap
make budget                      # remove all caps
```

### What a denial looks like

Set a cap below what any chat costs and try:

```bash
make budget CAP_TOKENS=1
make chat
```

The task fails with the reason in plain text — this is a real run:

```text
"status":{"state":"failed","message":{... "parts":[{"kind":"text",
"text":"monthly token budget reached"}] ...}}
```

The denial is a 429 from the proxy, sent before any upstream contact, and
it lands in the ledger too (you'll see several rows per attempt — the
runtime retries):

```text
2026-09-01T03:42:26 hello-world  ollama    qwen2.5:3b            0      0      0 denied   429
```

Budgets fail closed all the way down: if the proxy can't read the ledger,
budgeted credentials are denied; if it can't *write* the ledger, the whole
data plane returns 503 until it can — spend that can't be recorded doesn't
happen. Status-code meanings are in the
[FAQ](FAQ.md#what-the-planes-status-codes-mean).

### What governance does not cover yet

Only LLM calls through `governed-*` presets. The plain P2 presets still
exist and are still ungoverned. Tool/MCP calls have no gateway, permits, or
audit yet — that's the next slice (P4b), and it hasn't merged. Approvals
are further out (P4c). The
[P4a runbook](P4A-RUNBOOK.md#governed-vs-still-ungoverned) keeps the
honest table.

## Tools

`hello-tools` is the second agent `make up` creates. It reaches kagent's
bundled MCP tool server through `spec.declarative.tools`, allowlisted to a
single read-only tool (`k8s_get_resources`), backed by RBAC that cannot
read Secrets. Ask it something only the cluster knows:

```bash
make chat AGENT=hello-tools TASK='What pods are running in the ollama namespace?'
```

The tool call is real — CI proves it on every PR by planting a
randomly-named ConfigMap and requiring the agent to read it back. The small
local model's *summary* of tool output is less reliable than the call
itself; see the [FAQ](FAQ.md#the-tool-call-worked-but-the-answer-is-wrong)
before you trust a fluent answer.

## Going deeper

Each phase has a runbook with the full story — choices, caveats, and the
verification behind every claim:

- [P1 — hello-world on Kubernetes](P1-RUNBOOK.md): what `make up` does step
  by step, model choice, version pins.
- [P2 — hosted models](P2-RUNBOOK.md): every preset's caveats, the Copilot
  device flow, why Azure Foundry rides `provider: OpenAI`.
- [P3 — tools via MCP](P3-RUNBOOK.md): the tool server lockdown, how a real
  tool call is verified.
- [P4a — governed spend](P4A-RUNBOOK.md): the proxy's architecture,
  custody, pricing rules, operational notes.
- [CLI-PROPOSAL.md](CLI-PROPOSAL.md): the proposed `kaimahi create`
  command, including the case against building it.
