# P2 runbook — hosted LLM endpoints via ModelConfig presets

P1's hello-world agent thinks with an in-cluster Ollama model. P2 lets the
same agent think with hosted endpoints instead: each endpoint is a kagent
`ModelConfig` preset committed under [`k8s/models/`](../k8s/models/), and one
make target switches the agent between them. Nothing else changes — same
cluster, same agent YAML, same `make chat`.

> **⚠️ Spend is ungoverned in P2.** Switching the agent to a hosted preset
> sends every conversation to a billed API with no budget, metering, or
> ledger in front of it. That governance is Kaimahi's actual product and
> arrives in P4. Until then, treat a hosted preset like a live credit card.

## The presets

| Preset (`k8s/models/`) | Endpoint | Key Secret expected | Live-verified? |
|---|---|---|---|
| `ollama` | in-cluster Ollama (keyless, free) | none | **yes** — keyless e2e in CI |
| `github-copilot` | Copilot subscription (OpenAI models via api.githubcopilot.com) | `github-copilot-token` (via `make copilot-secret`) | **yes** — 2026-08-31, `gpt-5-mini`, A2A completed |
| `anthropic` | Anthropic first-party API | `anthropic-api-key` | not live-verified |
| `openai` | OpenAI first-party API | `openai-api-key` | not live-verified |
| `openrouter` | OpenRouter gateway | `openrouter-api-key` | not live-verified |
| `azure-foundry` | Azure AI Foundry, v1 GA API (edit `baseUrl` + `model` first) | `azure-foundry-api-key` | not live-verified |
| `openai-compatible` | any OpenAI-compatible base URL (template — edit first) | `openai-compatible-api-key` | not live-verified |

"Not live-verified" means exactly that: the preset is schema-valid against
the kagent 0.9.12 CRDs (CI proves this with a server-side dry-run every
run), but no real completion has been bought through it yet. A preset only
graduates to live-verified when an actual `make chat` completes through the
endpoint. See the GitHub section below for how the retirement of GitHub
Models reshaped P2's keyed verification target.

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
surface. Kaimahi pins Foundry's **v1 GA** API, which is a plain
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

## GitHub: Models is retired; the Copilot subscription path replaces it

The board's D7 ruling selected GitHub Models (authenticated via the GitHub
CLI) as P2's keyed live-verification endpoint. That service no longer
exists: **GitHub retired GitHub Models entirely on 2026-07-30** — playground,
model catalog, inference API, and BYOK, for all customers including existing
ones ([changelog](https://github.blog/changelog/2026-07-30-github-models-is-now-retired/)).
Verified directly on 2026-08-31: `https://models.github.ai/inference/...`
returns HTTP 410 (`github_models_retirement_brownout`) even with a valid
`gh` OAuth token. No preset for it ships.

What a GitHub subscription still provides (user ruling, superseding D7's
endpoint): **GitHub Copilot plans include API access to OpenAI and other
models** at `api.githubcopilot.com`, an OpenAI-compatible endpoint. The
`github-copilot` preset targets it:

```bash
make copilot-secret                 # GitHub device login -> Copilot token -> K8s Secret
make use PRESET=github-copilot
make chat
```

`make copilot-secret` (script: `scripts/copilot-secret.sh`) logs you in
once via GitHub's device flow (open the printed URL, enter the code),
caches that OAuth token 0600 under `~/.config/kaimahi/` (override with
`KAIMAHI_COPILOT_TOKEN_FILE`; if you logged in under the old tomte name,
migrate the cache once with `mkdir -p ~/.config/kaimahi &&
mv ~/.config/tomte/copilot-oauth-token ~/.config/kaimahi/` — or just
log in again), exchanges it at
GitHub's Copilot token endpoint, and stores **only the short-lived Copilot
token** in-cluster. Custody properties worth knowing:

- D7 asked for `gh` CLI login, but the gh CLI's own OAuth token is not
  Copilot-entitled — the exchange returns 403 (verified 2026-08-31). The
  device flow authenticates as the Copilot-entitled VS Code OAuth client,
  which is what Copilot tooling itself does. Same terminal-login UX, one
  extra browser approval on first run.
- The device-flow OAuth token never enters the cluster — only the
  short-lived exchange token does. All token bytes travel through 0600
  temp files and pipes; nothing touches argv, env listings, YAML, or
  logs, and no keyed call follows redirects. Fail-closed: a failed or
  empty exchange stores nothing.
- The exchanged token **expires** (typically within hours). When the agent
  starts failing auth, re-run `make copilot-secret` and then
  `make use PRESET=github-copilot` (the pod must restart to pick up the
  rotated Secret). An in-cluster auto-refresher is deliberately out of
  P2's scope — that is governance-plane territory (P4).
- `api.githubcopilot.com` is **not part of GitHub's documented public API
  surface** (GitHub's documented programmatic paths are the Copilot CLI/SDK
  and BYOK). It is the endpoint GitHub's own clients and sanctioned
  third-party integrations use, but treat it as subject to change without
  notice, and mind your plan's premium-request accounting.
- Model IDs are the Copilot catalog's (e.g. `gpt-5-mini`, `gpt-4o-mini`,
  `claude-*`, `gemini-*`); the preset defaults to `gpt-5-mini`.
