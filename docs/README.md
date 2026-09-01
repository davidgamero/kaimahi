# Kaimahi documentation

Kaimahi is an incubation project: an agent defined in one YAML file,
running on Kubernetes via [kagent](https://kagent.dev), driven from your
terminal, with a governance plane in front of it. Everything documented
here runs today and was run before it was written down. Where something
is demonstrated once rather than maintained, or schema-valid rather than
live-verified, the doc says so in those words.

## Start here

```bash
make up     # kind cluster + local model + kagent + two agents (~5-10 min)
make chat   # talk to the agent
```

[Getting started](getting-started.md) is the walkthrough: prerequisites,
what `make up` actually does, the agent YAML, and how to talk to it.

## By what you want to do

| I want to… | Read | The commands |
|---|---|---|
| Get an agent running and talk to it | [getting-started.md](getting-started.md) | `make up`, `make chat`, `make status`, `make down` |
| Use a hosted model (Copilot, Anthropic, OpenAI, Azure AI Foundry, OpenRouter, anything OpenAI-compatible) | [models.md](models.md) | `make model-secret`, `make copilot-secret`, `make use PRESET=…` |
| Give the agent a tool | [tools.md](tools.md) | `make chat AGENT=hello-tools …` |
| Put a budget on the agent's LLM spend, see the ledger, keep real API keys away from the agent | [spend.md](spend.md) | `make plane`, `make govern`, `make budget`, `make ledger` |
| Control which tools the agent can call, and audit every call | [tool-governance.md](tool-governance.md) | `make govern-tools`, `make tool-allow`, `make tool-audit` |
| Have a human approve a denied action with a bounded, expiring grant | [approvals.md](approvals.md) | `make approvals`, `make approve`, `make grants` |
| Let the agent post to Slack, one approved message at a time | [slack.md](slack.md) | `make slack-secret`, `make slack-mcp`, `make govern-slack`, `make slack-post` |
| Run all of this on a real cluster (AKS) instead of kind | [aks.md](aks.md) | `TARGET=aks`, `make aks-cluster`, `make aks-down` |
| Fix something that went wrong | [FAQ.md](FAQ.md) | |

Two more capability docs are being written in parallel lanes and are not
linked here until they land: cluster-level NetworkPolicy egress
(`docs/egress.md`) and inbound connectors that let the outside world
trigger an agent (`docs/inbound.md`).

The docs build on each other in roughly the table's order: tool
governance assumes the plane from [spend.md](spend.md) is deployed,
approvals assume tool governance, Slack assumes approvals. Each doc says
what it assumes at the top.

## What is governed today, and what is not

Governance is opt-in per agent. This is the one table that says what
the plane covers, kept in one place so it cannot drift between docs.

| Surface | Status |
|---|---|
| LLM calls through a `governed-*` preset | **Governed**: authentication, monthly budgets that fail closed, a ledger of every call, real keys held only by the proxy ([spend.md](spend.md)) |
| LLM calls through a plain hosted preset (`openai`, `anthropic`, …) | **Ungoverned, by choice.** Nothing meters it. Switch to a governed preset to govern it |
| Tool calls through `kaimahi-tools` (after `make govern-tools`) | **Governed**: authentication, a committed upstream table, tools-only protocol scope, a per-credential allowlist, every call audited ([tool-governance.md](tool-governance.md)) |
| Tool calls through `kagent-tool-server` directly | **Ungoverned, by choice.** `make govern-tools` to govern it |
| Approvals and bounded grants | **Governed**: a denial files a request, a human mints a grant bounded by expiry and/or use count, the grant lapses ([approvals.md](approvals.md)) |
| Slack posting | **Governed**: posting is not allowlisted, it is approved per use and audited with the grant id. There is no ungoverned Slack path in the repo ([slack.md](slack.md)) |
| The Slack MCP server's own endpoint authentication | **Not effective.** The plane injects a credential that the server (v1.3.0) does not enforce on its HTTP transport ([slack.md](slack.md#what-the-internet-egress-pod-means)) |
| Pod-level network egress (NetworkPolicy) | **Not built** as of this doc. The plane governs its seams; a pod that bypasses them can still open connections, and any pod can reach the Slack MCP server directly. A parallel lane is building this; until it lands, treat application-layer governance as enforcement for agents that use the seam, not containment of a hostile pod |
| Internet-facing *gateway* upstreams | **Not built.** Every committed tool upstream is in-cluster; going internet-facing needs a hardened dialer and SSRF protection that do not exist yet |
| Approval routing (Slack, email, per-approver identity) | **Not built.** The queue is CLI-only, and "who approved" is the admin bearer token, not a person |
| What an agent *sees* after a grant | **Lagging, not wrong.** Enforcement is immediate; the agent's discovered tool list updates on kagent's next RemoteMCPServer reconcile ([slack.md](slack.md#why-the-agent-is-never-the-one-denied)) |
| AKS | **Demonstrated once, not maintained.** One verified run on 2026-09-01, then deleted. CI stays on kind and keyless ([aks.md](aks.md)) |

## How these docs are organised

They used to be eight runbooks named for the phase that built them (P1,
P2, P3, P4a, P4b, P4c, P5a, P5b). That order was how the repo was
built, not how anyone reads it: to find "how do I govern tool calls" you
had to know it was P4b. The files are now named for the capability, and
the old filenames are short stubs pointing here so external links keep
working.

Each runbook mixed three things: how to use a capability, why it is
built the way it is, and what was verified when it shipped. The rule
for what moved where:

- **Kept, user-facing:** every command, everything you will see on
  screen, every caveat, gotcha and limitation, the "verified how" status
  of each claim (CI on every PR, live once, schema-valid only, not
  verified), and the design decisions you need in order to use the thing
  correctly (why Azure AI Foundry rides `provider: OpenAI`, why grants
  must be bounded, why the small model is pinned).
- **Moved out, historical:** alternatives-considered lists, porting
  provenance, surveys of what was not built, phase cross-references, and
  the narrative of the verifying run. All of that is in the delta sheets
  in [COORDINATION.md](COORDINATION.md), which is the project's record
  and stays the authoritative one.

If a caveat is missing from a capability doc that used to be in a
runbook, that is a bug in the restructure, not a decision. File it.

## Project docs, not user docs

- [COORDINATION.md](COORDINATION.md): the coordination board, decisions,
  and delta sheets. Owned by the coordinator session.
- [CLI-PROPOSAL.md](CLI-PROPOSAL.md): the proposed `kaimahi create`
  command, including the case against building it. Not built.
- [SCENARIOS.md](SCENARIOS.md): the delegation journeys that argue for
  the governance plane.
- [NAMING.md](NAMING.md): the project name and what is still owed before
  it is final.
