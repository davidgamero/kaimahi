# `kaimahi agent create` — working prototype

A first cut of the scaffolder from [CLI-PROPOSAL.md](CLI-PROPOSAL.md), built
against the billing journey in [SCENARIOS.md](SCENARIOS.md).

**Status: prototype.** It runs, it is tested, and it has been used to stand
up a real agent on a real cluster. Per D19 it is **not published to npm** —
internal use is `npx github:kaimahi-agents/kaimahi#<commit-sha>` — and `package.json` is
marked private so an accidental publish fails.

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

From outside a clone, per D19's "internal via npx github:":

```bash
npx github:kaimahi-agents/kaimahi#<commit-sha> agent create my-agent --instructions ./my.md
```

**Always pin the ref.** A bare `github:` spec resolves to the default branch
at that moment and executes it on your workstation, with your kubeconfig in
reach — the integrity problem `npx` has by default (CWE-494). Substitute a
reviewed commit SHA for `<commit-sha>`. CI fails if an unpinned spec appears
in the docs, so the instruction cannot rot back to the convenient form.

**The manifest lives at the repository root on purpose.** `package.json` sits
at the top level with `bin` pointing into `cli/`, because npm has no
subdirectory support for git installs — with the manifest inside `cli/`, the
`npx github:` invocation D19 mandates fails with ENOENT. CI installs the
repo into a scratch directory and runs the binary, so that stays true.

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
| **Tool allowlist is mandatory, and validated** | `--tools server` is refused; you must write `server:tool1,tool2`. Names must be identifiers and are quoted on emission, so a newline cannot close the YAML sequence and append a tool nobody reviewed (CWE-74, found in review). |
| **Generate, don't mutate** | Default is stdout. `--apply` is opt-in. |
| **Blast radius is `scripts/kube-guard.sh`'s call** | `--apply` delegates to the repo's existing guard, which checks the API server address as well as the context name and fails closed. The CLI used to match `kind-` on the name — cosmetic, and a context called `kind-prod` sailed through. `--yes` maps onto the guard's own `KAIMAHI_CONFIRM`. |
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

## Settled by D19

The proposal's open decisions have been ruled:

| Decision | Ruling | How this prototype reflects it |
|---|---|---|
| Publish to npm? | **Not yet.** Internal use via `npx github:kaimahi-agents/kaimahi#<commit-sha>`; publishing is a one-line change once D9's naming gates clear | `package.json` is `"private": true` and versioned `0.0.0-prototype`, so an accidental `npm publish` fails |
| CRUD boundary | **Scaffold-only.** `agent create` is the only command; R/U/D refuse and print the tool that already does the job | as built — `agent list` prints the `kubectl`/`kagent` equivalents |
| `up` / `install`? | **No.** The Makefile owns cluster bring-up | the CLI has no cluster-provisioning command |
| Node toolchain | **Accepted**, on condition it stays zero-runtime-dependency, with `make cli-test` in CI | zero dependencies; CI runs the tests and asserts the dependency count is still zero |
| Sequencing vs P4 | Moot — P4 shipped | the scenario scaffolds governed by default |

## Still open

D9's naming gates — the cultural read and trademark counsel — which now
gate publication. The org move (D16) made them more urgent, not less.

