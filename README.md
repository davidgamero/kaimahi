# tomte

Tomte makes agentic workflows accessible and safe to delegate.

Agents run on [kagent](https://kagent.dev) — declarative Kubernetes agents
defined as YAML (the Agent CRD is the agent-as-code topology artifact). Tomte
does not rebuild what kagent already ships (agent runtime, CLI, dashboard,
model providers, MCP tools). Tomte's product is the **governance plane** kagent
lacks: budgets and spend metering, approval workflows and blast-radius permits,
credential custody (keys never reach the agent), egress enforcement, and audit.

> "tomte" is used here as a working project name only; no trademark rights are
> claimed.

## What is built

- **P1 — hello world on Kubernetes**: a hello-world agent defined entirely in
  YAML (`k8s/hello-world.yaml`), running on kagent in a local kind cluster,
  driven end to end via CLI. Keyless (in-cluster Ollama model). Start with
  `make up && make chat` — full walkthrough in
  [docs/P1-RUNBOOK.md](docs/P1-RUNBOOK.md).
- Repository hygiene: license, CI, and the coordination process
  (`docs/COORDINATION.md`).

## What is designed (the arc)

1. **Hello world on Kubernetes** — *built, see above.*
2. **LLM-enhanced agent** — via kagent ModelConfig: Anthropic, OpenAI,
   OpenRouter, GitHub Models, Azure AI Foundry, any OpenAI-compatible base
   URL, local models.
3. **Connectors/tools** — via MCP, kagent's native tool mechanism.
4. **Governance** — mounted at kagent's existing seams: ModelConfig base_url
   pointed at a Tomte metering/enforcing proxy, an enforcing MCP gateway, and
   permits/approvals compiled down to kagent resources.

## Development

Work is coordinated through `docs/COORDINATION.md`. Every change lands via a
PR targeting `main` with CI green; verification claims are backed by actually
running the thing.
