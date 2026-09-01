# `kaimahi agent create` — working prototype

A first cut of the scaffolder from [CLI-PROPOSAL.md](CLI-PROPOSAL.md), built
against the billing journey in [SCENARIOS.md](SCENARIOS.md).

**Status: prototype.** It runs, it is tested, and it has been used to stand
up a real agent on a real cluster. It is not published, the npm name is not
claimed, and the open decisions in the proposal are still open.

## What it does

```bash
node cli/bin/kaimahi.js agent create --scenario billing --apply
```

Generates the agent-as-code YAML — Agent, model preset reference, tool
wiring with an allowlist — validates it, and optionally applies it. The
output is a file you own: the same YAML you would have written by hand.

```bash
make scenario-billing                          # governed (default)
make scenario-billing SCENARIO_MODEL=ollama    # ungoverned
make cli-test                                  # 12 unit tests, no cluster
```

## Grammar: noun-verb

`kaimahi agent create`, not `kaimahi create agent`. Groups by resource as the
surface grows (`agent create`, later `agent list`, `tool add`), matching
`gh pr create` rather than `kubectl create deployment`.

Only `agent create` exists. Asking for anything else prints the tool that
already does it:

```
$ kaimahi agent list
kaimahi: unknown command 'agent list'.
Only 'agent create' exists. Reading, updating and deleting agents are
kubectl and the kagent CLI's job:
  kubectl -n kagent get agents
  ...
```

That is the CRUD boundary made concrete: **C** is the only letter with a real
gap. R, U and D have shipped implementations, and the proposal's argument was
that competing with them spends credibility for nothing.

## Governed by default

The default preset is `governed-ollama` and the billing scenario's default
tool seam is `kaimahi-tools`. A scaffolded agent is metered, budgeted and
audited from its first call. Choosing an ungoverned path is possible and
warns on the way past:

```
WARNING: 'ollama' is ungoverned — no budget, no ledger, no audit in front of it.
WARNING: tool server 'kagent-tool-server' is ungoverned — calls are not
         allowlisted at the gateway and leave no audit trail.
```

## Safety properties, and why each exists

| Property | Why |
|---|---|
| **Never accepts a credential** — no flag, env var, or file | The generator emits Secret *references*. A scaffolder that can take a key is a scaffolder that can leak one into a file you are about to commit. |
| **Refuses key-shaped output** | Fail closed: if anything matching a known key shape reaches the manifest, writing stops. |
| **Tool allowlist is mandatory** | `--tools server` is refused; you must write `server:tool1,tool2`. An agent is never wired to every tool a server offers. |
| **Generate, don't mutate** | Default is stdout. `--apply` is opt-in. |
| **Non-local contexts need confirmation** | Applying to a non-kind context prompts unless `--yes`. |
| **Won't overwrite** | `--out` uses an exclusive create, so an edited file is never clobbered. |
| **Preflight on ModelConfig** | A missing ModelConfig is admitted by the API server and then fails to reconcile silently. The CLI checks first and prints the fix. |
| **Zero runtime dependencies** | `npx` executes remote code; the smallest dependency tree is the smallest supply chain. YAML is emitted by a hand-audited 80-line module rather than a library. |

The YAML emitter indents every line of a block scalar uniformly, so hostile
instructions cannot break out into sibling keys, and refuses multi-line flow
scalars outright rather than trying to escape them.

## What was verified, and what was not

On a dedicated kind cluster (`kaimahi-cli`; the board records `kaimahi-p1` as
contended), against kagent 0.9.12 with the P4a plane and P4b gateway
deployed:

**Verified working:**

- 12 unit tests covering YAML escaping, name validation, secret refusal, and
  allowlist enforcement.
- `--dry-run` passes a **server-side dry run against the live CRDs**.
- `--apply` produces an agent that reaches `Ready`.
- The preflight catches a missing ModelConfig and prints the exact remedy.
- **Ungoverned end to end**: the agent called `k8s_get_resources`, read the
  fixtures, and answered correctly — normal total $96, new total $222, an
  increase of $126, $96 of it roaming, against a roaming block active since
  2025-11 — then stated it could not contact the provider or request a
  credit without a human.
- **Governed plumbing**: LLM calls appear in the spend ledger with token
  counts and HTTP 200; tool calls appear in the gateway audit as
  `k8s_get_resources … allowed 200` under the agent's own credential.

**Not verified:**

- **A full governed conversation.** With Ollama, kagent, Postgres, the plane
  and the gateway all on one laptop kind node, the 3B CPU-bound model plus
  two extra hops exceeds the kagent controller's A2A client timeout. The
  governed calls themselves succeed — the ledger and audit rows prove it —
  but the round trip does not return in time. This is a capacity limit of a
  single-node local cluster, not a governance defect, and it needs either a
  larger node or a faster model to close out.

**Known weakness:** the answer quality depends heavily on prompt phrasing.
The first version of the scenario instructions produced a confidently wrong
conclusion — it called the roaming charge correct despite the active block.
Tightening the instructions into explicit steps fixed it. A 3B model is
doing real reasoning here only because the task was narrowed until it could.

## Still open

Everything the proposal listed: whether to publish to npm (which is a name
claim), where exactly the CRUD line sits, `up` versus `install`, and Node as
net-new runtime surface. This prototype does not settle any of them — it
makes them concrete enough to decide.
