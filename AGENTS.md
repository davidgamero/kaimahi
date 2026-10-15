# Repository guidance

Read [CONTRIBUTING.md](CONTRIBUTING.md), the
[development invariants](docs/development.md#invariants-to-preserve), and the
[repository map](docs/repository-map.md) before changing command behavior.

## Command consistency

- The `feat/suite-centered-tui` branch adds an experimental native-Orka suite
  console, read-only cluster `suite extract`, offline `suite show`, and private
  retained-placement `suite status` / root `run/chat --deployment`. Follow
  [the branch guide](docs/suite-centered-tui.md) for its bounded contract.
  Independent Agents are presentation wrappers, never automatically owned suites.
  Extraction writes new source only and preserves the original; unsupported
  behavior and any suite reservation refuse. Retained placement records are
  private rediscovery hints, never evaluation/export or Lift-reuse authority.
  Revalidate package, evidence, target, binding and owned UIDs before observation
  or member sends. Keep independent-agent refusals and all non-Orka limits.
  This explicitly adds native member observation/invocation and the console
  digest-Lift action described below; it does not restore old bundle/live-copy
  workflows or a retired command group. Root status remains runtime-only.

- This branch integrates #448/#449 image-http source/build/release/evaluation
  through the same root commands and split-pane console. Native remains default;
  image-http is explicit and uses Kubernetes workloads, not an Orka operator.
  Console `a` discovers AKS via exact configured endpoint matches and ACR role
  candidates; role observations are not network/pull or publisher authorization.
  Source/case review, explicit image publication/evaluation, reviewed Lift and
  retained status/run/chat use shared operation handlers. Revalidate private
  image evidence on reconnect as well as package/bindings/resource identity.
  Keep Orka installation separate and guarded; existing incompatible controllers
  are not upgraded. Helm inventory uses explicit status filters for Helm 3/4.

- Console creation separates execution-profile selection from inference.
  Local Copilot CLI is an explicit console host-execution choice with a selected
  local kind context, discovered CLI model and existing host login. It authors
  ordinary native source plus a private digest-bound catalog preference, never
  a public Copilot runtime or credential. Console run/chat revalidate local
  context, source and model availability; no cluster Tasks, tools or candidate
  evaluation proof. Source edits require review and explicit H reconnect.
  Quickstart host modes remain retired; native eval/Lift still require Provider
  bindings and runtime evidence. Copilot preferences never authorize deployment.
  Agent creation defaults to Orka: name → inference/model/instructions → Create
  agent. Alternative execution profiles are selected through Change execution on
  final confirmation, not a mandatory initial question.
  Default the source folder from the name; show Save to on final confirmation
  with optional Change location, not a mandatory workspace step.
  Subscription selection starts in fuzzy search, shows name plus GUID,
  and prefers the last selected available subscription by saved identity.
  Arrow keys navigate filtered results; typing (including j/k) edits search.
  Foundry resource/deployment, Ready Provider and inference-source pickers share
  that immediate fuzzy-search behavior. AKS/ACR and named-destination forms use
  fuzzy resource completion; Enter resolves the highlighted match, never a query.
  Host-chat Foundry subscriptions share name/GUID and last-selection preferences.
  Empty panes offer New agent directly, without lifecycle explanations. Context
  discovery proceeds despite unavailable private placement records; refusals stay
  visible and those records never authorize sends. `t` picks the focused side's
  contexts with fuzzy search. Local offers existing kind access and confirmed
  initialization via local up; remote `n` discovers AKS under the active Azure
  subscription and confirms private kubeconfig retrieval. Imported access is
  separate from agent ownership and does not overwrite the default kubeconfig.
  Console mouse clicks/wheel use rendered hit regions and shared keyboard actions;
  overlays block underlying actions and review paging still gates confirmation.
  Source/case review yields mouse reporting to terminal drag selection; keyboard
  paging remains gated and mouse navigation resumes on close. Review formatting
  separates trusted label styles from sanitized, indented authored multiline text.
  Create review styles labels separately from values. Foundry connection setup
  discovers matching independent Providers and Secret key names, then explicitly
  confirms create-only Provider/Secret setup. New keys come from a named host
  environment variable via stdin, never argv or review/log output. Existing
  resources are not overwritten; discovery/setup is not model-access proof.
  Independent live Orka rows offer confirmed shared interactive chat again;
  suite-owned resources still require retained placement revalidation.
  Live read-only Inspect is available without retained metadata and checks observed
  UID/resourceVersion; it never authorizes sends. Independent chat preserves selected
  kubeconfig/namespace and chooses an available loopback result port. Operation
  loading is animated/labeled and uses metadata-only progress events, not raw
  subprocess logs. Runtime confirmation text belongs in the visible dialog body.
  Console Lift starts with a fuzzy published-version picker, newest known first.
  Publication history is bounded private retrieval metadata, never evidence or
  placement authority; selecting older placements does not reorder publication
  history. Manual digest entry remains advanced. Shared planning revalidates all
  selected artifacts/evidence; no registry-wide latest-tag guessing.
  With no known publication for an opened source, console Lift starts explicit
  publication directly and resumes Lift selectors with its returned exact digest.
  Build/push consent, evaluation evidence and reviewed deployment stay separate.
  Guided console Lift selects copied context/namespace/inference values without
  requiring kmx.yaml. Runtime/Foundry/auth-Secret setup, ACR login, publication,
  evaluation and Apply each require explicit consent. Image bindings derive from
  reviewed packaged capabilities and selected Secret references. Registry HTTPS
  reachability and role observations are not publisher/pull authorization.
  Shared handlers own effects; no automatic retry of writes/model sends. Offline
  layouts/manual bindings remain Advanced; no default local OCI directory prompt.
  Guided runtime recovery may create a separate pinned Orka v0.2.0 release only
  in new installation/runtime namespaces after compatible shared-CRD, existing
  controller watch/isolation, admission-scope and rendered-object collision checks.
  Use unique release/fullname, --skip-crds and namespace-local result-reader RBAC.
  Legacy/cluster-wide controllers refuse; no CRD upgrade, adoption, rollback or
  automatic cleanup. Native runtime/worker inspection follows the selected target
  namespace. Runtime warnings appear above actions, red with NO_COLOR text.
  Runtime prerequisites first show an Okay acknowledgement; acknowledgement
  performs no setup. Kagent remains operator-installed. New KMX Orka installs pass
  installed/runtime-version annotations alongside ownership labels; annotations
  alone never grant deployment or teardown authority.
  Environment choices say LOCAL/REMOTE; model choices identify both the model and
  its inference connection. Lift target details use hanging indentation and may
  include best-effort exact-endpoint Azure resource-group/subscription metadata.
  Agent lists default to recent authored-file/non-status resource changes first;
  unknown timestamps sort last. S selects per-pane sorting; f fuzzy-filters without
  changing artifact identity. Counts/numbering reflect filtered display order.
  Missing source hints are reported separately, never represented as live agents.
  Source-only entries are local authoring files, not ownership of the selected
  kind cluster. Agent rows display observed/declared inference with the name.
  The default kind pane lists only Ready, non-deleting Agents observed in its
  selected context, plus revalidated Ready image placements. Stale/unobserved
  placement hints and source files never populate that cluster list. `w` toggles
  a separately labeled source-files view; explicit open/create enters authoring.
  Orka readiness is not a running worker Pod or a fresh-answer claim.
  Offer Ready openai Providers and existing Foundry deployments through read-only
  discovery; manual model entry is an advanced fallback. Discovery is not cluster
  credential provisioning. Native source pins the selected model; image source
  keeps capabilities and defers concrete inference to environment bindings.
  Backspace navigates from pickers/start-of-text, Alt+Left always navigates back,
  preserving drafts. Shortcut text is orange with readable NO_COLOR output.

- Follow the [command conventions](docs/command-conventions.md), the implemented
  interface in [docs/kmx.md](docs/kmx.md) and the
  [command alignment record](docs/command-semantics.md). Track outstanding
  renames and decisions in [#301](https://github.com/kaimahi-agents/kaimahi/issues/301).
  Proposed spellings are not implemented capabilities.
- Canonical context commands are `kmx context show` and
  `kmx context use <context>`; canonical local setup/teardown commands are
  `kmx local up/down`. Root `up`, `down`, the AKS `lift` alias and `lift down`
  are removed outright, not compatibility routes or retirement stubs. The old
  context-switching `ctx` alias remains removed; new `kmx ctx [--check]` only
  lists named workspace destinations, never selects or remembers one. Current
  deployment is digest `kmx lift`; cloud and runtime setup
  remain `kmx aks up/down` and `kmx orka install/status`.
  The AKS group is a hidden compatibility route, not a public root domain.
  Guided quickstart is `kmx quickstart --interactive` (`-i`); the old wizard
  command is a retirement stub. `kmx chat` is always interactive and no
  longer accepts `--interactive`. `kmx targets` is the read-only capability view.
  Plane administration, model overlays, migration and their demo are removed;
  do not restore or regroup their commands.
- Named destination selectors live in versioned workspace-root `kmx.yaml`, not
  source `workspace.yaml` or published content. `eval/lift --to NAME` resolves
  concrete context/namespace/runtime expectation/inference-or-bindings/registry
  values once and passes those copies onward; never reread config during an
  operation. Explicit flags override saved fields. `evalTarget` is evaluation-only;
  Lift never guesses a destination. Explicit-reference Lift without `--to`
  remains workspace-independent. Runtime stays package-owned. `ctx` is an
  offline list; optional checks are local context-name/anonymous registry reads,
  not runtime/inference/Secret probes. `targets` remains runtime capabilities.
  Reference Lift still requires a digest and evidence. No-reference `lift --to NAME`
  composes missing Orka evaluation/publication/Lift from one frozen workspace and
  selector plan. Require saved publishRepository and destination evaluationTarget;
  automatic disclosed promotion, one scoped confirmation, four default stage lines
  with verbose/JSON details. Exact-version private completion observations authorize
  GET-only skips, never Apply/reuse/adoption/repair. Keep kagent out of composed
  Lift until its candidate-scoped placement and ownership-safe cleanup are qualified.
- Everyday operations are root `create/chat/run`; live inspection is `orka show/list`.
  Legacy `bundle` operations are removed outright without aliases/stubs.
  `kmx agent` is removed entirely, without an alias or retirement stub.
  Root `status` and `orka status` retain the same table-only Orka runtime report;
  do not add root bundle status or a suite observer.
- Root `create <name>` authors one tool-free native suite member offline
  by default: Orka unless `--runtime kagent` selects exact v0.10.3 Go/linux-amd64.
  `--runtime agentsessions` separately authors a built-in chat
  profile pinned to ca85f47a2ec17ca6e16029cfa69419cfe29442b3 (engine chat), with
  empty required images and complete ordered cases. Prepare/load/ordinary push
  support it; root eval requires explicit --development-host FILE for literal
  loopback, unauthenticated operator-observed chat. It loads no cluster settings,
  sends exact config once per fresh Session, requires a complete five-record
  qualified journal and full ordered pass, then exports private runtime-specific
  proof. Exact-S --evaluation push accepts that proof offline. Cluster selectors,
  mixed-runtime proof, tools, lift/chat/status and live create refuse.
  Operator declarations are not authenticated host/build attestation; cleartext
  journal retention is disclosed. Session replay cannot create candidate proof.
  Optional --runtime-version must match that commit; --kagent-runtime refuses.
  Runtime is member-owned KMX-private metadata, not a public AgentSuite field.
  Root eval supports the packaged kagent profile with author-owned --bindings;
  evaluated push consumes matching runtime-specific full-passing suite evidence.
  Suite status/retirement remain unavailable; digest lift uses the common release
  seam with runtime-matching evidence and author-owned kagent bindings. Kagent
  lift is create-only, refusing occupied primaries/children even with matching
  evaluated UIDs; evidence never crosses runtimes.
  Private runtime-specific kagent prepare/place/observe helpers are separate from
  W250's common release-lift/dispatch/evidence seam. They create new ModelConfig
  and Agent objects or reuse a complete caller-retained observation read-only;
  occupied unrecorded/changed targets refuse, never adopt/update/rollback.
  Observations are in-memory placement proofs, not persisted/evaluation receipts.
  Placement observes the literal auth mode without native sends; evaluation
  requires unsecure before admission and each send, trusted-proxy refused.
  Orka-only consumers refuse that profile. RemoteMCP/Python remain unsupported.
  Missing runtime in old source stays Orka only. Create and `push [--workspace DIR] REF` share nearest-ancestor
  `workspace.yaml` discovery; `--workspace DIR` selects an exact root without
  fallback and create initializes a missing marker there without prompting.
  Without a workspace, terminal create asks `Start one here? [Y/n]`; nonterminal
  or redirected use refuses with a `--workspace` hint. Invalid nearest markers
  fail closed. Authoring requires exact `--model` and file-valued `--instructions`;
  a second member refuses without changing authored files. Help/completion is inert.
  Workspace push names its absolute root and prepared OCI manifest digest on
  stderr, then copies that exact graph and authored cases.
  `suite push DIRECTORY REFERENCE` remains extracted-directory transfer without
  discovery and no longer accepts `--workspace`. The native member image
  requirement is empty. Neither offline command evaluates, deploys, builds images
  or loads cluster settings. Workspace evaluation is root `eval`; deploy-only release lift is root
  `lift <ref>@<digest>` with explicit private `--evidence`; no-reference workspace
  `lift --to NAME` separately composes the missing stages.
  User `agent.yaml` readers/writers/receipts, `bundle` commands,
  `create --live` including previews/wizard, live-only flags, root run bundle
  mode and `--bundles` flags are removed outright. Unknown routes/flags refuse
  before config. Old Orka/kagent/AgentSessions bundle receipts are unreadable;
  no migration or saved-file/resource deletion. Suite status/retirement remain
  unavailable. Quickstart uses native source → full eval → exact local publication
  → reviewed Lift Apply/Verify, then a separate fresh answer Task. Console Ready
  is placement only. Chat `/lift` and console Lift print only
  `Lift moved: kmx lift <ref>@<digest> --to-context … --evidence …`.
  No release GUI/live-copy fallback. Wizard tools/host modes and live tools/inference
  edits are retired; host Foundry/Copilot chat controls remain independent.
  Preserve public AgentSuite/ToolProvider implementation-bundle graph/build/pull,
  extracted-directory transfer and alpha `pkg/kmx` interfaces. PortableAgent typed
  scaffolding used by native preparation is not a persistent old reader.
  Native suite-owned resources refuse legacy invocation/adoption/copying.
  Private packaged-native preparation and ownership-safe placement feed full-workspace
  evaluation. The existing Orka CI shard configures three cases against the pinned
  runtime/model: two text cases and one open-ended llmJudge case using the same
  AIKit workload through an owned keyless loopback forward. Private metadata
  independently checks judge identity/configuration and both scores. Job/case/outer
  timeouts are unchanged; the 660s outer budget bounds three sequential cases,
  not three complete 5m waits. Hermetic runner tests alone are not live judge proof.
  Internal PrepareReleaseLift plans an exact digest from a registry or read-only
  OCI layout, with bounded ingestion before validation and no source fingerprint.
  It requires explicit private full-passing native evidence, default exact observed
  subject equality and GET-only absent-or-receipt-bound owned-resource inspection.
  Internal explicit promotion requires identical packaged identity/runtime and a
  Ready openai Provider declaring the same model; destination resources must be
  absent. Source evidence remains distinct, not destination evaluation or signer
  authorization. Planning makes no admission, writes, Tasks or placement records;
  summaries contain identities and cannot mutate retained inputs. Kagent uses
  --bindings, never --inference, and derives namespace from the anchored bindings.
  Explicit promotion projects only approved physical identities/refs/installation
  fields; package/member/model/profile/provider kind/AuthMode and the full
  controller plus Go image digest/platform inventories remain exact. Unknown
  fields compare by default. Go workload image proof is observed after creation;
  mismatch leaves partial resources with no successful placement authority.
  No lift Tasks/model calls, fake Provider or destination evaluation is implied.
  ApplyReleaseLift
  is guarded and single-use, rechecks artifact/evidence/target/owned proof, preflights
  both creates after the guard, and only conditionally creates or exactly reuses.
  Partial outcomes retain acknowledged UIDs and readiness separately; no update,
  adoption, retry, rollback or cleanup. VerifyReleaseLiftPlacement rechecks an opaque
  success before caller invocation. Created/recreated UIDs require fresh evaluation
  before later receipt-bound reuse. Root lift supports registry/local --from-layout,
  --plan and text/JSON, with explicit selectors or named --to and private --evidence; --promote
  discloses source-only evidence. Runtime/model are packaged, never overridden.
  Help/completion is inert. General suite transfer is unchanged.
  Native inference is openai-type Provider only, not host Foundry/Copilot execution.
  Private native passing receipts retain only full ordered passing metadata, export
  once without replacement outside workspace/cache trees, and bind the private
  receipt-file push consumer to exact S plus offline packaged coherence. There is
  a full-workspace native evaluation CLI with one fresh never-retried Task per
  declared run of each ordered case. Optional KMX YAML runs/minPasses defaults
  to one run/all passes, bounded to 100 runs plus the offline receipt budget.
  Each run receives its own case-timeout budget, including judging/attribution;
  repeat preflight states the summed run-time budget excluding placement.
  Any unknown vetoes the threshold. Only
  completed attributable assertion failures are countable; execution errors
  still abort. AgentSessions development evaluation shares this repeat policy.
  Private repeated rows retain ordinal/policy and actual outcomes; default
  one-run encoding stays unchanged. Field names/defaults/validation follow
  [AgentSuite §8.2.2](agentsuite/spec.md#822-case-documents-and-assertions):
  minPasses requires runs and both are positive integers. KMX additionally caps
  runs at 100 under the spec's consumer resource-limit allowance.
  Root push requires latest completed private exact-S evidence by default;
  `--evaluation FILE` explicitly overrides lookup without fallback, and
  `--unevaluated` explicitly retains ordinary publication. Explicit REPO:TAG
  remains unchanged; bare push requires saved publishRepository and derives only
  its tag as `s-<full-S-hex>`. Root eval makes --out optional and registers managed
  proof only after evaluation, Orka cleanup, final input checks and evaluator/output
  closes. Managed 0700/0600 per-workspace/per-S immutable completion records embed
  existing canonical receipts outside workspace/cache. Per-S sequences select latest;
  corruption refuses. Capacity preflight blocks new eval registration, not existing
  proof lookup; staging is ineligible. Visible complete records remain eligible after
  post-link persistence errors with explicit uncertainty. OS-held workspace flock
  excludes concurrent root eval/push without a stale-lock blockade. Private convenience
  evaluators and extracted-directory suite push retain their old contracts. Unsigned receipts do not prove authentication
  or drift absence.
  Any presence of a native suite ownership annotation reserves desired/live
  resources against legacy adoption, copying, bundle authorization and retirement.
  Registry workspace publication shares unsigned suite provenance defaults/flags and records Git source only from
  fingerprint-bound authored inputs matching HEAD; local layouts get no referrers.
- Root `eval --development-host FILE` separately supports the packaged
  AgentSessions chat inventory under the development boundary above; every
  cluster/inference/bindings/result selector (including explicit defaults) refuses.
  Missing development selection never falls back. Sessions proof is refused by
  lift and receipt diff; there is no registry or caller-auth integration.
- Root `eval` evaluates the full packaged Orka/kagent workspace inventory with
  a destination context (explicit `--to-context`, named `--to` or saved evalTarget)
  and optional new private `--out`; Orka uses --inference
  provider:NAME, kagent uses author-owned --bindings FILE, never both. Namespace
  must match kagent bindings and kagent refuses --result-port. It uses local
  flags and a nonpersistent operational hook. Unsupported/budget/output checks
  run before config; one fresh outer guard covers pair placement and all Tasks.
  Fail/unknown/drift/cancel/output errors stop later sends with no partial export.
  No model/runtime override, subset/content mode or standalone package
  reference is accepted. Authored llmJudge is supported for Orka/kagent and explicit AgentSessions development hosts with
  explicit --judge-base-url and optional --judge-key-env (environment name only).
  The fixed openai-chat-completions-v1 request style uses max_completion_tokens
  (1024), stream=false and no temperature override, including the pinned AIKit
  judge. No request-style flag, model inference or error fallback. Two independent
  scores must agree on the threshold side;
  unavailable/malformed/split results stay unknown. Judgment shares the existing
  per-run deadline and attribution is rechecked afterward. Root eval discards
  reasoning and omits judge endpoints from recovery commands. Private passing
  rows bind authored model/version, request style, threshold, policy, endpoint
  configuration hash, both scores and aligned modelMatches observations. Returned
  model identity matches only the exact authored model or model+"-"+version,
  never prefixes/substrings/case folding/whitespace normalization. Old judge rows missing the style require
  fresh evaluation. Text-only receipt wire shape is unchanged; older readers
  refuse judge rows. AgentSessions uses the same judge transport and evidence validation.
  Offline `eval diff` reads strict full-passing native Orka/kagent suite
  evidence only. Old normalized receipts are unreadable. `eval from-sessions`
  remains independent private reference extraction/export; RecordedExecution,
  legacy bundle replay/evaluation/judge wiring and their consumers are removed, with no native
  manifest consumer. Preserve private filesystem/current identity ResourceVersion
  and Secret metadata bracketing. Hermetic tests are not live qualification.
  Runtime-core assertions and llmJudge remain retained with their test groups;
  native judge wiring does not enable recorded candidate execution.
  The checkout-only native qualifier runs in the existing exclusively owned
  Orka shard, after legacy model proof, with private captures and metadata-only
  artifacts. Its second exclusively owned kind cluster has one pinned Orka in
  orka-system and its own inference Provider/Secret; it shares the source AIKit
  endpoint through a fixture-only private-network NodePort, never a host mapping
  or another model workload. Registry --promote qualifies GET-only planning,
  absent-pair CLI creation, independent destination readback and one fresh Task;
  model mismatch refuses with zero admission/write/Task attempts. Source evidence
  is not destination evaluation. Keep the shard's existing timeout budget.
  A passing run qualifies that fixed workspace scenario; hermetic
  tests alone do not establish runtime/model execution.
- Keep **Create → Prove → Lift** as the local-to-remote story. **Lift** is the
  chosen agent lifecycle verb: retain digest `kmx lift`,
  and use Lift in corresponding help rather than renaming the
  operation to deploy. Deployment remains a technical description of its effect.
  Root `lift` is deploy-only packaged-suite lift, not an AKS provisioning alias.
  Use `kmx aks up/down` for AKS.
- Prefer singular resource/domain groups and explicit actions. Avoid introducing
  another synonym for an existing operation. CLI, chat and console should use
  the same operation vocabulary where their scopes match.
- Reuse the existing operation handlers. Keep Cobra syntax, terminal rendering,
  runtime capabilities, target mechanics and infrastructure ownership separate.
  A renamed command must not silently change runtime, target, ownership or
  supported operations.
- Update command registration, help, completion, relevant chat/console labels,
  generated next/recovery commands, primary docs and CHANGELOG in the same slice.
  Record changed defaults and compatibility/removal policy. Keep old routes
  callable until an announced transition unless an outright removal is explicitly
  approved; record that removal and its replacements. Notices belong on stderr.
- Preserve report/artifact/answer stdout and exit contracts. Test meaningful
  selection, refusal and compatibility behavior, not just matching help strings.
  Help and completion must not perform operational mutations.
- A context supplies access. A target includes the selected runtime, cluster
  identity and namespace. Inference is independent. Unknown is not absent;
  ready is not answered or evaluated; retry is not result retrieval.
- Experimental `pkg/kmx.AgentEnvironment` is setup-only and unwired:
  `Up(EnvironmentSpec)` establishes a logical `Environment`, not a context,
  exact target, runtime selection, inference binding or teardown authority.
  Bare `kmx local up` is only its current conceptual mapping. Do not claim CLI
  integration, inspection, persistence, recovery or down semantics until those
  capabilities have production implementations and evidence.
- Preserve historical release records and explicit compatibility tests when
  updating current examples. Do not bulk-rename historical text or serialized
  state. Generated recovery commands must preserve selectors and shell quoting.

## AgentSuite evaluation storage

- Keep the draft `agentsuite/spec.md` implementation-neutral and focused on the
  current format. Put KMX details, migration notes, and design history in
  implementation documentation, issues, or the changelog. Shared JSON and
  identity rules belong in common sections; eval bindings belong in §8.2.
- Public embedded evaluations are optional (`SHOULD` for producers), rooted at
  `evals/<agent>/index.json` and bound through the agent's `evals` reference.
  Indexes bind ordered JSON cases with JCS digests; no evals remains conformant.
  Follow `agentsuite/spec.md` §8.2 and its closed schemas. The public validator
  checks storage and identity only, never executes assertions or claims a pass.
- Preserve KMX's authored assertion `id`/`type`/operand spellings when mapping
  cases; public JSON identities are not private YAML/framed receipt identities.
  Results remain external to the evaluation tree and bind the exact OCI suite
  manifest. Do not infer KMX migration from this public format; its private
  `metadata/kmx/eval/` authoring and receipt consumers remain unchanged.

## kagent version boundary

- Exact v0.10.3 Go/linux-amd64 packaged profiles use author-owned bindings,
  unsecure auth only, one fresh blocking A2A Task per declared case run, never retried.
  Trusted-proxy refuses before admission/send. Digest Lift creates absent objects
  only with runtime-matching evidence; occupied primaries/children refuse.
  No Python/RemoteMCP, standalone chat/run, installer/discovery, suite status or
  retirement is implied. Preserve Orka defaults and workspace missing-runtime behavior.
- CI retains exact v0.10.3 bootstrap, raw upstream Tool smoke/direct single A2A
  MCP proof, separately tool-free packaged two-case eval/evaluated push and
  source-withheld digest Lift with occupied-repeat refusal. No legacy receipt,
  status or judge claim. Final build/live execution are needed for qualification.

## Candidate evaluation and explicit cleanup

Public Orka workspace eval uses fresh S/run candidates, never adoption/update/reuse.
Full passing evidence exports only after UID/RV-safe cleanup, exact owner-UID child
closure and final source/shared-input checks. KMX-created Provider/Agent/Task carry
full run annotations; Orka descendants do not inherit them. Startup reports only;
unknown reporting never blocks a fresh eval. `eval cleanup [run-id]` lists without
a run, otherwise previews frozen UIDs and requires confirmation even on local kind.
Unanchored or unverifiable orphans refuse deletion/completeness claims. No journal,
lease, automatic sweep or publication blockade. Private Quickstart/console stay
canonical; kagent/Sessions/judge behavior and all existing timeouts are unchanged.

## Verification

The browser documentation preview renders the root README and `docs/*.md`
directly, plus `agentsuite/spec.md` while it remains local. Keep Markdown
authoritative: navigation and theme code may link to maintained content but must
not copy it. Keep VitePress output,
caches, and `node_modules` untracked. Documentation changes retain the existing
link, README-front-door and repository-map checks and run
`npm --prefix docs run docs:check` when they affect the rendered site. The
current exact alpha framework pin is a draft choice, not approval to publish;
custom-domain and versioned-site changes require a separate review.
Image workspaces use explicit `--execution image-http` without native model/runtime
overrides. Image release indexes carry exact suite and image graphs; publish the
final pointer only after completeness. Root image eval/lift use separate evidence
and shared guarded apply, not a parallel reconciler. Retained image placements
support root run/chat and suite status; shared console uses the same review pane.
Ownership is suite/member/instance/target-specific plus receipt UID/spec fences.
Private packaged cases remain until #493 adopts #497; no speculative spec migration.

Run checks appropriate to the changed paths as described in CONTRIBUTING.md.
Command changes normally require `go test ./cmd/kmx`, affected operation-package
tests, documentation links, repository-map and legacy-runtime checks. These
inventory checkers use tracked files, so include intended additions in the index
when checking the complete change. Update AGENTS.md when implemented conventions
change so subsequent work follows the same rules.
