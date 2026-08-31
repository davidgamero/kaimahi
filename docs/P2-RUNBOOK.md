# P2 runbook — hosted LLM endpoints via ModelConfig presets

P1's hello-world agent thinks with an in-cluster Ollama model. P2 lets the
same agent think with hosted endpoints instead: each endpoint is a kagent
`ModelConfig` preset committed under [`k8s/models/`](../k8s/models/), and one
make target switches the agent between them. Nothing else changes — same
cluster, same agent YAML, same `make chat`.

> **⚠️ Spend is ungoverned in P2.** Switching the agent to a hosted preset
> sends every conversation to a billed API with no budget, metering, or
> ledger in front of it. That governance is Tomte's actual product and
> arrives in P4. Until then, treat a hosted preset like a live credit card.

## The presets

| Preset (`k8s/models/`) | Endpoint | Key Secret expected | Live-verified? |
|---|---|---|---|
| `ollama` | in-cluster Ollama (keyless, free) | none | **yes** — keyless e2e in CI |
| `anthropic` | Anthropic first-party API | `anthropic-api-key` | not live-verified |
| `openai` | OpenAI first-party API | `openai-api-key` | not live-verified |
| `openrouter` | OpenRouter gateway | `openrouter-api-key` | not live-verified |
| `azure-foundry` | Azure AI Foundry, v1 GA API (edit `baseUrl` + `model` first) | `azure-foundry-api-key` | not live-verified |
| `openai-compatible` | any OpenAI-compatible base URL (template — edit first) | `openai-compatible-api-key` | not live-verified |

"Not live-verified" means exactly that: the preset is schema-valid against
the kagent 0.9.12 CRDs (CI proves this with a server-side dry-run every
run), but no real completion has been bought through it yet. A preset only
graduates to live-verified when an actual `make chat` completes through the
endpoint. See "GitHub Models is retired" below for why P2's planned keyed
verification target no longer exists.

## Storing an API key (stdin-only)

Keys go in Kubernetes Secrets and nowhere else — never in YAML, ConfigMaps,
argv, environment listings, or logs. The make target reads the key from
stdin only:

```bash
make model-secret NAME=anthropic-api-key
# Paste the key, press Enter, then Ctrl-D.
```

The pipeline strips the trailing newline before the key reaches
`kubectl create secret --from-file=api-key=/dev/stdin`; a newline left in
the Secret would corrupt the Authorization header on every request. To
rotate, `kubectl -n kagent delete secret <name>` and re-run.

## Switching the agent

```bash
make use PRESET=anthropic      # apply the preset, point the agent at it, wait Ready
make chat                      # same CLI conversation as P1
make use-ollama                # back to the keyless local model
```

`make use` applies `k8s/models/$(PRESET).yaml` and patches the one field
that matters on the Agent: `spec.declarative.modelConfig`. The kagent
controller rolls the agent deployment; the target waits for both the
rollout and the Agent Ready condition. Create the preset's Secret **before**
switching — an agent pointed at a ModelConfig whose Secret is missing will
not become Ready.

Alternatives considered for switching (recorded for the PR): a kustomize
overlay per preset (heavier, no live cluster needed — but we always have a
live cluster here), editing `k8s/hello-world.yaml` per switch (mutates the
P1 demo artifact), and one Agent per preset (N agents to keep Ready, and
the demo's point is one agent changing its mind, not many agents).

## Azure AI Foundry rides `provider: OpenAI` — deliberately

kagent 0.9.12 has an `azureOpenAI` provider, but its `apiVersion` field is
**required**, and that field belongs to Azure's legacy per-version API
surface. Tomte pins Foundry's **v1 GA** API, which is a plain
OpenAI-compatible endpoint with **no** api-version parameter. The two are
incompatible, so the `azure-foundry` preset uses `provider: OpenAI` with the
v1 base URL:

```yaml
openAI:
  baseUrl: https://YOUR-RESOURCE.openai.azure.com/openai/v1
```

Set `model` to your **deployment** name (not the upstream model name). The
same pattern covers OpenRouter and any other OpenAI-compatible endpoint —
at 0.9.12 there is no provider-specific CRD support for them, and none is
needed.

## GitHub Models is retired (was: the D7 verification path)

The board's D7 ruling selected GitHub Models (authenticated via the GitHub
CLI) as P2's keyed live-verification endpoint. That service no longer
exists: **GitHub retired GitHub Models entirely on 2026-07-30** — playground,
model catalog, inference API, and BYOK, for all customers including existing
ones ([changelog](https://github.blog/changelog/2026-07-30-github-models-is-now-retired/)).
Verified directly on 2026-08-31: `https://models.github.ai/inference/...`
returns HTTP 410 (`github_models_retirement_brownout`) even with a valid
`gh` OAuth token.

Consequences:

- No `github-models` preset ships — committing a preset for a dead endpoint
  would be a standing trap.
- The planned `gh auth token → kubectl create secret` flow buys nothing
  anymore; it is not shipped either.
- GitHub's own migration pointer is Microsoft/Azure AI Foundry, which the
  `azure-foundry` preset already covers.
- The Copilot API (`api.githubcopilot.com`) is **not** a substitute: it is
  undocumented, token-exchange only, and out of bounds per the board.

Which endpoint replaces GitHub Models for keyed live verification is a
user/coordinator ruling (supersedes D7); until then every hosted preset
stays marked not-live-verified.
