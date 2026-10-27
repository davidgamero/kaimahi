# Markdown agents: run, organize, test

Start with a Copilot `.agent.md` or plain Markdown file. No companion files,
workspace marker, container or cluster are required for an ordinary tool-free
file preview. The persistent Copilot SDK instance path is explicit `--type md`;
its execution requires the installed authenticated Copilot CLI.

## Layout

```text
weather/
  weather.agent.md
  weather.tools.yaml
  weather.tests.md
  tools/weather-api.yaml       # optional explicit tool index reference
  tests/weekdays.tests.md       # optional explicit test index reference
```

Match companions by filename stem, not the frontmatter display name. A literal
`agent.md` uses `agent.tools.yaml` and `agent.tests.md`. Directory input selects
exactly one root `*.agent.md` or `agent.md`; ambiguity refuses. No recursive
agent selection or implicit tool/test scanning occurs.

```sh
kmx run weather.agent.md 'What should I wear?'
kmx import weather.agent.md --into ./weather
kmx validate ./weather
kmx inspect ./weather -o yaml
kmx test ./weather --provider copilot
kmx run ./weather --provider copilot
kmx chat weather
kmx stop weather
```

Import copies the original agent bytes and existing referenced companions into a
new directory; absent companions become valid empty templates. Existing output
refuses. No model request, candidate command or approval occurs during import.
Partial failures report an incomplete destination rather than deleting user files.
Unknown execution settings are preserved on import and refused by the persistent
runner until mapped. Imported MCP declarations never start a server automatically.

## Tool declarations

The agent's `tools` list selects named capabilities. The sibling YAML defines
their implementations; declarations are not executable grants.

```yaml
schemaVersion: 1
tools:
  go_version:
    description: Read Go version
    command: go
    args: [version]
    parameters: {}
    execution:
      timeout: 5s
      max-output-bytes: 1024
      approval: granted-scope
    validation:
      args: [version]
      exit-code: 0
      stdout-pattern: '^go version '
      timeout: 5s
```

Root tool indexes may add `files: [tools/weather-api.yaml]`. Each leaf uses the
same schema without further includes. Tool names are unique across all selected
files. At most eight bindings are supported. Only agent-requested names are
activated; missing definitions require a supported host binding or explicit review.

## Tests

`weather.tests.md` contains human-readable context and strict test blocks:

````markdown
---
schemaVersion: 1
files: [tests/weekdays.tests.md]
---

# Weather tests

```yaml agent-test
id: explains-unavailable-weather
input: Explain what you do when weather is unavailable.
assertions:
  - id: explains
    type: contains
    value: unavailable
```
````

Root cases run first, then referenced files in listed order. Case IDs are globally
unique; leaf test files cannot include more files. Missing tests means untested.
Tool-using cases require deterministic `mocks.tools` with name, description,
parameters and ordered calls (`arguments`, `result`); there is no live fallback.
`toolCalled`/`toolNotCalled` assertions inspect actual mock calls. A mismatch fails
and stops the invocation. `--case ID` reports a partial result. Recordings remain
inline in these test blocks; external result-file references are not implemented.

References are relative to the agent directory, confined, non-symlinked and
explicit. Duplicate paths/names/IDs, nested includes and missing files refuse.
Each companion is bounded to 128 KiB; each tool/test file set to 1 MiB and 32
referenced files; a test suite has at most 32 cases.

## Suggest, approve, activate

```sh
kmx tools suggest ./weather --model MODEL
kmx tools review PROPOSAL_ID
kmx tools review PROPOSAL_ID --apply --select TOOL --yes
kmx tools approve ./weather
kmx tools approve ./weather --apply
kmx run ./weather --provider copilot --update
```

Suggestions receive the definition and bounded PATH-name availability, not
executable paths. Candidates/probes never execute during suggestion. Acceptance
updates tool names and writes the companion; incomplete multi-file saves are
reported, never treated as approval. A changed companion invalidates old proposals,
approvals and test identity. Separate approval reviews executable resolution/hash,
fixed argv, constraints and probes. A probe proves only its declared condition.

Calls run directly as the local user with inherited credentials, in a temporary
working directory, not a sandbox. Use fixed `go -C /project test ./...` arguments
for project commands. String operands require full-match patterns or enums.
Completed nonzero exits stay failed results with bounded sanitized stdout/stderr;
both streams share a budget. Known credential-shaped, invalid or incomplete
output is withheld. No automatic retry occurs.

## Console and lifecycle

`kmx console --type md --directory ./weather --definitions` opens source inventory.
File-pane `n` authors an agent with empty companions. Suggestions, source acceptance
and executable approval are separate steps. Approval requires explicit `y` after
review; Enter never grants. Next actions open editors, tests, chat or reviewed
update. Agent tests are not compatibility probes; approval does not start an agent.

Chat supports `/show`, `/edit`, `/edit-tests`, `/test`, `/tests`, `/diff`, `/update`,
`/reset`, `/auto-test on|off`, `/cancel` and `/quit`. Attached chat keeps cached
execution separate from editable source/tests. Test-only edits change test identity;
tool edits require reapproval and explicit update. Local grants/cache/history are
private KMX state and are never copied as portable execution authority.

## Packaging boundary

The current `suite map` / Markdown Deploy bridge supports text-only one-member
Orka mapping and requires native evaluation. Local tool grants and mock results
never qualify deployment. Frozen directory compilation and package-bound testing
remain the shared follow-up under #669/#670; this source layout is not itself an
AgentSuite artifact or deployment proof.
