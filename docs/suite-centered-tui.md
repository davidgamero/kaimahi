# Suite-centered TUI and live-agent extraction

**Status: experimental branch implementation, not released.** Branch
`feat/suite-centered-tui`, based on main `5b499ae` (including #499).
The [CLI reference](kmx.md) remains authoritative for implemented commands.

## Test this branch

### Headless terminal qualification after main reconciliation

On 2026-10-15, a real PTY driven through a headless xterm emulator completed
native Orka guided Lift on the rebased integration branch (`d5ce5b5` plus the
result-port/completion-dialog fixes). Binary SHA-256:
`d8043165f012f515958555851e8ab6e91ca1eb776d2e8d83866056007bcab0c9`.
Rendered screen snapshots verified fuzzy destination, namespace, inference and
ACR selection; explicit publisher login; the suggested repository; source and
both case reviews; rejection of blank consent; and the shared workspace pipeline.
Two real arithmetic cases passed against qwen2.5:3b on AKS, candidates were cleaned,
the exact suite was published to ACR, and Lift created a retained placement.
A subsequent TUI Run returned `8`. Independent cluster readback matched Agent and
Provider UIDs, retained status succeeded, and terminal settings were restored.

The rehearsal exposed fixed result-port collisions and completion/answer output
hidden in the removed detail pane. Pipeline and retained member execution now
select an available loopback port, preserving bind verification. Completion,
failure and answers have visible dialogs. Earlier failed evaluations stopped
publication and cleaned their candidates; a diagnostic interrupted run left its
acknowledged deployment for inspection. Model availability was repaired separately
before a new explicitly confirmed candidate, never credited as passing proof.

Private captures and the successful summary remain under the operator's
`/tmp/opencode/headless-lift-pz5j0_69` test directory. Dedicated test deployments
and registry artifacts are retained. This qualifies the native path at 180×52,
not image building, all terminal sizes, or automatic runtime installation.

### Main reconciliation

Integration branch `feat/suite-centered-tui-main` rebases the preserved console
checkpoint onto main `2238a93`. New native source now uses the same
`PrepareWorkspaceLift` / `LiftWorkspace` operation as root workspace Lift after
guided destination and repository selection. The initial console path uses the
selected target for evaluation and placement; the backend retains separate
evaluation/destination support. Native versions use full-S-derived tags, managed
passing evidence and disposable UID-safe candidate cleanup. The TUI presents the
backend's frozen effects/target disclosure and returns the typed confirmation to
the existing guard. Completed stages retain GET-only skip semantics.

An optional retained invocation handoff copies canonical evidence to a private
immutable file before the pipeline removes its temporary evidence. Completion
records never authorize Apply or substitute for invocation verification. A repeat
that only verifies completion does not fabricate a new retained handle. Native
standalone Prove and Publish also use public candidate evaluation/managed proof.
Image-http remains a separate profile over shared release Apply/Verify; native
pipeline semantics are not applied to image or kagent implicitly. Main's Helm
version detection and descriptor-based artifact contracts supersede the original
branch implementations. Earlier live qualifications below predate this rebase.

### Lift interaction audit

**Implemented follow-up:** console Lift now uses a guided destination-first flow
after version selection (or immediately for unpublished source). It discovers
contexts/AKS, namespaces, runtime prerequisites, Ready inference Providers or
Foundry setup, ACR candidates with observed pull roles, HTTPS registry reachability,
explicit ACR publisher login and running OCI-export-capable buildx builders.
Repository/version defaults derive from the agent name. Source/cases are reviewed
before new publication/evaluation. Image bindings are generated privately from
the chosen model/credential, declared packaged capabilities, agent auth Secret
and image-pull authentication. Random agent-auth Secret creation is separately
confirmed. No handwritten environment JSON or local OCI directory is required
on the normal path.

Known proof is checked by the shared planner; Evaluate now executes the full
reviewed source inventory with exact published-subject equality, and source-only
promotion remains explicit. Runtime setup, credential writes, publication,
evaluation and final Apply each retain separate consent. Failures offer read-only
retry where appropriate, destination reselection or Advanced; ambiguous effects
and model sends are never automatically retried. Advanced retains external
artifacts, evidence files and offline layout selectors.

Remaining limits: existing namespaces only; non-ACR registries use manual host
entry and external credential-store login; no automatic Azure role assignment,
AKS network/pull repair, builder provisioning or model-access attestation. New
image evaluation requires absent names. A different published source revision
requires matching source or imported evidence. Guided flow tests cover shared
planning/apply, image bindings, consent, fuzzy selection and safe access checks.
A fresh live guided AKS rehearsal was blocked by unavailable prior temporary
workspace/kubeconfig; earlier CLI/PTY qualification is not this flow's live proof.

The table below records the pre-guided audit that motivated this implementation.

Current implemented forms still expose low-level operation inputs. The published
version picker and unpublished-source publication handoff do not yet constitute
a fully guided destination-to-registry journey:

| Step | Current way forward | Gap |
|---|---|---|
| Published version | Fuzzy known-version picker, latest known first | Local history only; external versions require advanced reference entry. |
| Unpublished source | Direct confirmed publication, then exact-digest Lift selectors | Publication starts before destination/registry discovery. |
| Native publication | Typed registry/tag, optional OCI directory, evidence path and HTTP choice | Selected ACR is not prefilled; local-layout mechanics occupy the normal path. |
| Image publication | Registry reference defaulted from console options, typed buildx builder, confirmation | No builder discovery or integrated registry access checks. |
| Native destination | Fuzzy names from existing kmx.yaml | Empty config has no creation action; selecting a context with t/a does not create or resolve a named destination. |
| Image destination/inference | Typed environment JSON path | No generated/reviewed binding flow from selected context/Provider/Secret. |
| Evidence | Prefilled when known, otherwise a file path | No compatibility-aware evidence picker or guided Prove-and-resume action. |
| Promotion | Typed yes | Must explain source-only proof and offer an explicit choice rather than an unexplained boolean. |
| Runtime setup | Separate v/I actions | Missing runtime does not guide setup and return to the pending Lift journey. |
| AKS/ACR | Separate fuzzy discovery/selection with role observations | Active subscription scope; publisher network/auth and node pull are not checked by discovery. |
| Final Apply | Exact plan review and typed target confirmation | Keep the shared guarded/single-use implementation. |

**Intended next flow:** choose destination/context → namespace/inference → choose
registry (prioritize observed AKS pull-role candidates, allow fuzzy search of
available registries) → check host access and offer explicit login remediation →
review publication/build if needed → obtain compatible evaluation proof → review
source-only promotion if applicable → review/apply/verify → offer fresh chat/run.
Default repository naming from the agent, showing an editable name rather than
requiring a complete reference. Keep a distinct advanced offline/local-layout
route. A local OCI directory is a filesystem artifact store, not an ACR image or
an AKS destination, and should not be a required normal-flow question.

Native Orka publishes an AgentSuite definition with no required member image.
Image-http builds and publishes the runnable image plus suite association. The
guided journey can orchestrate these stages, but root digest Lift remains
deploy-only and GET-only planning remains unchanged.

Registry status must separate host DNS/TLS/reachability, current publisher
authentication, repository write authorization, and AKS network/identity pull
access. A successful registry read or Azure role observation cannot prove push
or node pull; publication and independently observed workload pull establish
those respective outcomes. Do not introduce an undisclosed probe image/write or
automatic role assignment under a connectivity check.

The kind pane is now a live-only Ready inventory by default: local source hints,
non-Ready/deleting Agents and missing placements are excluded. Retained native
rows must match the observed Agent UID; image placements require current readback.
`w` switches to LOCAL SOURCE FILES for editing/Prove/Publish; opening or creating
source selects authoring explicitly. This separates local files from resources
actually observed on the selected kind cluster. Ready is not execution proof.

Agent lists show inference beside numbered names, total/matching counts, and
recent edits/creation first. `S` changes sort order for the focused pane; `f`
fuzzy-filters agent/suite names and inference. Search and selection survive polling.
Unavailable saved source folders are excluded from agent rows with an aggregate
diagnostic; catalog hints are retained. Earlier tests accidentally wrote temporary
source hints to normal state; affected tests now isolate KMX_HOME. These source
hints never represented agents on the selected kind cluster.

Lift (`l`) opens a fuzzy publication picker with the latest known publication
selected. Older versions retain their own digest/layout/evidence hints; observed
placements do not advance publication recency. Mouse and keyboard selection share
the same path. New source without publication offers Publish first; manual exact
references are an advanced fallback. Current source differences are shown when
the published suite digest is known, and shared Lift planning still revalidates
artifact/evidence identity before admission or writes.

For opened source with no known publication, Lift now skips the empty version
picker and starts publication directly. Successful publication resumes the Lift
form with its exact digest; cancellation/failure stops the journey. This does not
skip evidence or deployment confirmation. Source/case review yields mouse events
to the terminal for ordinary drag-copy and uses structured, indented multiline
blocks with highlighted labels. Keyboard paging remains required while reviewing.

Mouse clicks select tabs, agents and menu/picker rows; wheel input navigates lists
or scrolls details/reviews. Modal hit regions are derived from wrapped visible rows
and block click-through. Continue/Back/Cancel are clickable; ordinary typed consent
and full source-review paging still apply. Independent live Orka rows now expose
shared interactive chat without requiring extraction; suite-owned rows still need
a retained placement. Source-only native rows must first be placed to chat.

Foundry creation confirmation has label/value styling and a connection setup action.
It discovers an exact matching independent Provider, checks Secret key presence,
and offers confirmed create-only setup using a named environment variable for an
absent Secret. No key is displayed or passed in argv. Existing mismatches refuse;
partial resources remain if creation fails. Successful setup is not evaluation.

Empty local/remote panes show **No agents or suites** and a selectable **New agent**
row. If observation fails, the pane says unavailable rather than claiming absence.
The `t` context picker searches configured access and saved private imports;
local selection also discovers kind clusters missing kubeconfig and offers runtime
initialization through the shared local-up operation. Remote `n` browses AKS using
the active Azure subscription, confirms the selected identity, retrieves private
access and verifies its endpoint. A broken placement record is reported separately
from live contexts and cannot authorize invocation.

### Required end-to-end validation TODO

#### Demonstrated on 2026-10-15

**Native AKS follow-up:** after explicit operator authorization, the old
non-Helm Orka 0.1.3 installation was removed, including its 17 CRDs, three
Agents, three Providers, 14 Tasks and tool definition. A private non-Secret
snapshot was retained; the old datastore PVC and Secrets were preserved.
Pinned Orka v0.2.0 harness-v2 installed successfully on AKS with a new datastore
and result-reader RBAC. A separately created Ollama workload served
`qwen2.5:3b`. The exact locally evaluated quickstart suite was published to ACR,
GET-only promotion planning succeeded, and guarded Lift created an absent
Provider/Agent pair. A fresh remote Task answered `4` to an arithmetic question;
retained status revalidated their UIDs. Source evidence remains source-only, not
a destination evaluation. This closes the previously recorded native-install
blocker for this target; it is an explicit replacement, not an automatic upgrade.

### Console UX follow-ups

Follow-up verification: live independent Orka chat reproduced a fixed result-port
collision. Interactive chat now selects an available loopback port and retains
bind verification/single-connection result handling. A real color-enabled PTY
completed a fresh independent-agent answer and returned with /exit. Suite-owned
AKS inspection also passed with an empty local placement catalog: a dedicated
read-only modal rechecks UID/resourceVersion, displays public configuration, and
explains missing retained metadata without granting chat/run authority. The old
Inspect action wrote to the now-hidden Source preview. Runtime confirmation text
is now in the modal body rather than a clipped title. Background operations show
animated labeled loading with metadata-only phase events; no raw Secret-bearing
command logs are streamed. Search focus highlights the input while typing.

- [x] Trace why inspecting a suite-owned Agent on the operator-selected AKS
  target fails. Resolve the exact configured context by endpoint, reproduce the
  Inspect action, and distinguish missing private retained-placement metadata
  from unavailable live resource access. Explain “no retained placement” in the
  UI and allow supported read-only inspection without implying invocation or
  ownership authority; preserve required revalidation before sends or writes.

- [x] Investigate why interactive chat fails for non-extracted independent agents.
  Reproduce through both `c` and the action menu; trace target/namespace access,
  confirmation, live-agent lookup, inference, Task creation and result retrieval.
  Restore supported independent-agent chat without requiring extraction; preserve
  suite-owned resource refusals and show actionable failure details in the TUI.

- [x] When Find agents is active, move the visual focus/highlight to its search
  input and show the typing cursor clearly. Distinguish the active search field
  from the selected result, then restore normal list focus when search ends.

- [x] Investigate the stuck **Install pinned Orka** screen displaying only `>`
  and Continue. Clearly show required confirmation and an actionable setup step;
  execute the shared installer, stream sanitized setup logs/progress and outcomes,
  and resume Lift. Determine runtime/version requirements from the selected
  AgentSuite profile and validated supported pins, preserving artifact identity.

- [x] Every discovery/fetch operation needs an animated loading bar and a clear
  activity label, including Lift targets, Azure enrichment, namespaces, inference
  connections and registries. Keep the UI responsive and cancellable; transition
  to results or an explicit error rather than appearing frozen during reads.

**Completed follow-up qualification:** pinned chart v0.2.0 explicitly supports
multiple releases with distinct installation/watch/runtime namespaces. The guided
recovery action now checks existing pinned controller isolation, shared CRD schemas,
webhook scopes and rendered resource collisions before confirmed --skip-crds
installation. A live second install on the dedicated kind qualification cluster
passed full native evaluation, evaluated layout publication, receipt-bound Lift
and a fresh answer. Original Orka deployments remained Ready. Both namespaces and
test resources are retained; no automatic cleanup or older-version migration is
claimed. Schema/version/watch incompatibility remains a refusal, not an invitation
to force a new namespace. Runtime/worker discovery follows the selected namespace.

The model/connection wording, LOCAL/REMOTE tags, Lift detail indentation and
best-effort exact-endpoint Azure resource-group/subscription enrichment below are
implemented. Runtime warning text is displayed above actions in red (or labeled
plain text under NO_COLOR).

- [x] Investigate and qualify a second KMX-managed Orka installation alongside
  an existing installation, using a new or non-overlapping namespace. Verify
  shared CRD/version compatibility, disjoint controller watch scopes,
  cluster-scoped RBAC/webhook/resource collisions and independent ownership and
  cleanup. Namespace separation alone does not isolate incompatible CRDs or
  controllers. Where coexistence is supported, offer explicitly confirmed setup
  from runtime prerequisites and route agent placement to the new installation.
- [x] Show runtime-prerequisite warnings (including “installed Orka does not
  satisfy…”) above the available actions, styled red with readable NO_COLOR
  output. Offer **Set up KMX-managed Orka** in a new/non-overlapping namespace
  when coexistence checks permit it, then resume the pending agent/Lift flow.

- [x] Fix Lift destination-list indentation: wrapped labels and subsequent detail
  lines should be indented beneath their target. Enrich matching Azure targets
  with resource group and subscription name/GUID through read-only discovery when
  available; missing Azure metadata must not change or hide the target identity.

- [x] Revisit inference selection language: distinguish choosing an environment,
  choosing an inference connection and choosing a model/deployment. Tag context
  choices LOCAL or REMOTE. Replace vague “Ready Provider” headings with explicit
  model/inference terminology, explaining what the selected connection supplies.

Creation defaults to a native Orka AgentSuite and goes directly from name to
inference/model selection. Final confirmation shows Orka as the default and offers
Change execution for image or local Copilot alternatives. Provider/context choices
use hanging indentation on wrapped labels.

Local Copilot creation is available as an explicit execution choice when the
console has selected a local kind context. Model discovery uses the installed
CLI; run/chat reuse its existing host-login transport. Authored source remains
native and portable, while the host preference is private and digest-bound.
It is not a cluster placement, evaluator or Lift authority. `u`/`c` confirm host
execution; edits require source review and `H` reconnect. Remote contexts refuse.

- [x] Render keyboard shortcuts in a distinct text color: orange for now,
  preserving readable non-color output.
- [x] Backspace returns to the previous creation step while retaining entered
  values. In text fields it navigates at the start; Alt+Left always goes back.
- [x] Replace the combined exact-model/image-http text prompt with separate
  guided execution-profile and inference selections. Offer supported features,
  available connections and applicable endpoint/Foundry setup through existing
  Connect inference operations. Distinguish host inference from cluster-native
  bindings and keep setup explicit; manual strings should be a fallback.

Implemented creation asks for the agent name, then offers an execution picker followed by
Ready openai Provider selection or Azure subscription → Foundry resource →
deployment discovery for native models. An advanced manual fallback remains.
After instructions, **Create agent** saves editable source to a folder named
after the agent. Final confirmation shows **Save to** and offers **Change location**;
there is no mandatory workspace-directory step. Creating source does not start
execution or provision infrastructure.
Subscription rows show name plus GUID and accept fuzzy typing immediately.
The last-used available subscription is first and selected by default; preferences
never change Azure CLI defaults. Arrows navigate filtered results, while Backspace
edits search until empty, then navigates back.

Picker audit: Foundry resources/deployments, Ready Providers and inference-source
lists now share immediate fuzzy search. AKS/ACR and named destination form fields
match names/details fuzzily and accept the highlighted result with Enter. Chat's
Foundry resource/deployment, Copilot-model and agent pickers already used immediate
search; its subscription rows now also show GUIDs and share last-selection state.
Quickstart's older agent/model selectors still use arrow/j/k navigation, and small
execution/confirmation menus remain navigation-only. The retired in-place inference
editor is not an active route and remains retired.
Image creation declares capabilities and defers concrete inference selection to
evaluation bindings. Discovery reads configuration only; it neither provisions
Foundry resources nor copies credentials. Foundry selections explicitly explain
the separate cluster Provider/Secret setup requirement. Confirmation authors
source only. Esc cancels; interrupted discovery does not write source.

Investigation: `consoleAzureChoices` already discovers subscriptions, Foundry
accounts and successful deployments. The old inference pane models cluster
Providers, Ollama and Foundry choices, but its save handler deliberately refuses
retired in-place agent edits. Reuse discovery and selection concepts, not that
mutation path. New creation should first select execution, then select an
existing compatible connection/model or an explicitly supported setup action.
Native source records the exact chosen model; destination/credential bindings
stay separate. Image source records inference capabilities and binds its concrete
model at evaluation/placement. Host Foundry/Copilot access does not imply that
an AKS workload can use those credentials or endpoints.

### Earlier integrated qualification

The integrated uncommitted tree (base `5b499ae`, image work from #449
`794d449`, including #448) was built and exercised with real Docker/buildx,
kind, authenticated ACR and AKS. This is one-off qualification, not continuous CI.
Final rebuilt binary SHA-256:
`a7acdda6e2d1ad3172ee50a8c9377be27341c52f5bfc1d8e74a4cf9c2b3d3c77`.
Fresh-process remote console status/run/chat was repeated on that binary.

- Fresh dedicated kind quickstart installed SHA-256-pinned Orka v0.2.0
  harness-v2, configured Ollama `qwen2.5:3b`, passed the complete scaffold case,
  published an exact local suite, Lifted and returned a separate fresh answer.
  The first attempt exposed Helm 4's removed `--all`; explicit status filters
  fixed installation without changing existing-release collision checks.
- Image-http publication built a unique member with requested/required
  attestations and published a complete release index to ACR. Kind pulled it
  using a namespace-scoped registry credential, evaluated all cases, Lifted,
  and returned an authenticated fresh response.
- Source-only kind-to-AKS promotion created absent ConfigMap/Deployment/Service
  resources. Independent Pod `imageID` and a `Successfully pulled` event matched
  the exact ACR image; neither cluster preloaded it. Fresh retained run/status
  succeeded. Repeated occupied promotion and changed-model evidence refused.
- Actual PTY console exercised AKS endpoint discovery, ACR role discovery and
  selection, image publication, source/case review, fresh local evaluation,
  reviewed Apply/Verify, separate fresh invocation, and then source-only remote
  promotion and a fresh remote answer. A fresh console process reconnected to
  AKS, revalidated status, ran a member, entered shared chat, received an answer,
  returned to the console and exited.
- The image execution used a **controlled model fixture**, checking exact model,
  credential and instructions. It proves real container/build/registry/cluster
  transport, not external model quality. Native quickstart used the real local
  model. Image-http uses Kubernetes directly; Orka installation was qualified on
  kind. Existing incompatible AKS Orka was preserved, not upgraded.

Private workspaces, bindings, evidence and retained test resources remain under
the operator's temporary state and dedicated test namespaces. Initial diagnostic
runs and failed/unknown candidates also remain; no automatic cleanup occurred.
The smoke runner now supports source-only promotion and independently asserts
actual pull events and running image identity. A cached-image diagnostic correctly
failed that stricter check before a uniquely built image passed.

Additional live drills refused a missing image-pull Secret before placement and
cancelled a fresh evaluation after Deployment creation without exporting evidence.
Before/after UID/spec snapshots of existing AKS Orka Deployments, Agents and
Providers were identical. The partial candidate remains for inspection.
Remote absent-native-Orka installation requires a separately approved clean
target. Actual registry-authorization denial (with a present but unauthorized
credential) remains distinct from the demonstrated missing-Secret refusal.
The complete checklist below remains open for those two qualifications.

- [ ] **Quickstart → kind → AKS/ACR → running AgentSuite image**, exercised on
  the exact integrated branch through the real TUI:
  - Run quickstart in an exclusively owned kind cluster; create the suite member
    and require a fresh, readable local answer, not merely Ready resources.
  - Select an AKS cluster through the guided flow and discover its connected
    ACR candidates. Confirm the chosen registry and distinguish publisher access
    from the cluster's image-pull access; never infer attachment from a similar name.
  - Read packaged runtime requirements and destination state. Offer separately
    confirmed Orka operator/controller/Helm preparation on a supported absent
    target; resolve incompatible existing installations through an explicit
    operator-approved replacement plan, not silent upgrade or deletion.
  - Build the selected image-backed AgentSuite member, publish the exact image
    and suite association to ACR, and retain the immutable runnable image digest.
    Integrate #448/#449; native zero-image publication alone does not satisfy this.
  - Evaluate the exact supported subject and review the remote placement and
    evidence. Keep build/publication/setup explicit stages of the guided journey;
    root digest Lift stays deploy-only and `--plan` remains GET-only.
  - Lift to the selected AKS target without preloading the image. Independently
    verify the workload pulls from the selected ACR, its running image identity
    matches the published digest, and the member returns a fresh remote answer.
  - Restart/reconnect the console, verify placement identity again, and exercise
    cancellation, stale-evidence and missing-pull-access refusals without adopting,
    overwriting or deleting the original agents or unrelated infrastructure.
  - Record exact commits, runtime/image/suite identities, test results and retained
    resources privately; keep credentials, private endpoints and execution payloads
    out of committed evidence.

The demonstrated image-backed path above supersedes the earlier native-only
artifact round trip. It does not close the remaining qualification items.

Build `bin/kmx` with `make`, then open the live console:

```sh
bin/kmx console
```

The native one-member console is implemented on this branch: create/open source,
review every case, edit files through `$EDITOR` (one executable; defaults to vi),
evaluate, publish to an OCI layout or registry, review digest Lift, retain verified
placement and invoke/reconnect a member. Tab switches Source/Members/Cases/
Artifacts/Destinations/Activity. `R` opens the shared bounded run projection.
`/` offers the same actions by command name. Old root agent verbs remain.

The console uses the full-screen alternate buffer with persistent bordered
LOCAL (left), REMOTE (right), and operation-progress panes. Each side contains
its actual suite/member inventory and selected details. Left/right focuses a
side; up/down or j/k navigates its entries, and PgUp/PgDn scroll details. Tab/Shift-Tab
or brackets select detail views. Enter opens a capability-aware actions menu;
`i` opens source/case review. Forms, extraction review, exact Lift plans, help
and the command palette are framed overlays over the dashboard. Palette and
destination-field completion use up/down, Tab and Enter. Long reviews must page
through the visible content before confirmation. `NO_COLOR` keeps pane layout
and textual selection markers. Public demo mode is removed. Context discovery
uses kubeconfig and existing preferences; only the selected local and remote
contexts are queried. Bounded read-only refresh runs every five seconds, pauses
during reviews/effects, preserves selection and reports failures as unavailable.

Use prepared Orka destinations with existing inference and result access. In an
authored workspace, create operational `kmx.yaml` (or use `d` and name that file):

```yaml
version: 1
evalTarget: dev
targets:
  dev:
    context: kind-demo
    namespace: orka-system
    runtime: orka
    inference: provider:local
  prod:
    context: remote-demo
    namespace: orka-system
    runtime: orka
    inference: provider:inference
```

The destination Provider must declare the exact packaged model for promotion.
Start `bin/kmx console --workspace ./my-suite`; pin cluster inventory with
`--local-context` / `--remote-context`. Open/create (`o`/`n`), actions (`Enter`), inspect (`i`),
edit (`d`), Prove (`e`), Publish (`p`), Lift (`l`), status (`s`), one Task (`u`) or
chat (`c`). `t` selects the cluster inventory contexts inside the console.
Create accepts instruction text or `@path` to read a file. Publication separately
confirms that authored instructions/cases will
be copied. Evaluation and Apply name their destination and require explicit
context confirmation. Lift requests explicit source-only promotion when needed.

Extraction is available from a standalone row (`x`) or directly:

```sh
bin/kmx suite extract existing --context kind-demo --namespace orka-system \
  --name extracted-copy --workspace ./extracted-copy --plan
bin/kmx suite extract existing --context kind-demo --namespace orka-system \
  --name extracted-copy --workspace ./extracted-copy
bin/kmx suite show ./extracted-copy
bin/kmx console --workspace ./extracted-copy
```

The initial extractor accepts only exact representable native Orka source:
Provider reference, exact/default model and inline or ConfigMap-backed instructions.
Additional behavior fields refuse. Source is bracketed by repeated GETs. The new
directory must not exist; scaffolder cases are examples, not derived proof. Writes
use exclusive authoring and may retain an incomplete directory on filesystem
failure; no concurrent files or deployed resources are removed.

Console Apply automatically retains successful native Orka placement privately.
CLI users opt in with `lift ... --retain-placement` (incompatible with `--plan`).
Use the printed ID:

```sh
bin/kmx suite status --deployment ID
bin/kmx run --deployment ID --prompt 'Hello'
bin/kmx chat --deployment ID
bin/kmx console --deployment ID
```

Records retain exact artifact/evidence retrieval paths; these must remain
available. They are unsigned operator-local observations, not portable credentials
or deployment-reuse authorization. Every observation/send revalidates them against
the artifact and current target. Changed resource identities refuse. They do not
implement same-operation crash recovery, history or retirement. Image placements
use separate runtime-specific records with the same revalidation boundary.
Native timing remains bounded and Task sends are never automatically repeated.

**Verification:** full Go suite passed during implementation; focused extraction,
placement, console flow/race and PTY restoration tests cover this branch. The
integrated console test executes real source/evidence/publication/Lift operations
with simulated cluster/result transport, then requests one fresh answer after
rediscovery. The live qualification above separately covers native quickstart and
image kind-to-AKS execution. Multi-member operations, tools and Kubernetes watch
streams remain separate. Refresh uses bounded polling; progress reports real stage/case
transitions and resource outcomes, not fabricated transfer percentages.

## Runtime prerequisites and registry proof

`kmx lift REF@sha256:DIGEST --to-context CTX --check-runtime` retrieves the exact
package and reports the required runtime plus ready/absent/incompatible/unknown
destination state. It requires no evaluation evidence and performs no admission
or installation. `--plan` and ordinary digest Lift keep their original contracts.
The console exposes `v` for this check and `I` for separately confirmed Orka setup,
using `orka install --provider -` and its existing Helm/CRD/controller/result-access
workflow. Incompatible or foreign existing installations refuse; no implicit
upgrade, removal or cluster replacement is performed. Inference remains separate.

The branch has demonstrated real native workspace creation, ACR push, source-free
digest pull and offline conformance validation. The operator-selected AKS target
was observed running Orka 0.1.3, so runtime preflight correctly reported it
incompatible with native Orka v0.2.0 harness-v2. No native AKS Lift/answer success
is claimed for that target. Registry transfer is not runtime qualification.

## Design and follow-on scope

The remainder records the original intended design and wider milestones. The
implemented native/image slice above is narrower than the eventual multi-runtime
suite workspace; its qualification report supersedes the original milestone status.

## Outcome

Make AgentSuite the console's primary lifecycle object and agents its members.
A one-agent workflow is a one-member suite, not a competing lifecycle. Keep
agent creation, inspection, run and chat directly accessible. Preserve
**Create → Prove → Lift**, with publication visible between proof and Lift.

Existing independently deployed agents remain useful without being silently
adopted. Offer **Extract AgentSuite** to read one supported live agent into a
new editable workspace, leaving the deployed original intact. Do not call this
action migration: it neither moves the original nor grants authority over it.

The first complete demonstration targets one tool-free native Orka member on
prepared local and remote destinations. The view model supports member lists
from the start; that does not make multi-member authoring/evaluation supported.
Native default profiles require no member image. Image execution extends the
same UI through #448/#449, rather than becoming a second console.

## Baseline and gaps

- #484 supplied shared source/case review, execution consent, full evaluation,
  exact local publication and reviewed Lift Apply/Verify for console creation.
  Quickstart can send a separate fresh answer; console Ready is placement only.
- #495 removed the old user bundle lifecycle and interactive live-copy Lift.
  Current console Lift prints a CLI move notice. Old file readers, receipts,
  live mutation editors and adoption semantics are not an extraction foundation.
- #498 supplied named destinations in workspace-root `kmx.yaml`. Resolve these
  once through shared code. `ctx` lists destinations; `context use` selects
  Kubernetes access; `targets` reports runtime capabilities.
- #499 supplied explicit native suite judge evaluation. Show case/assertion
  verdicts and declared judge identity, not private reasoning or endpoint values.
- The current console inventories live Orka Agents in local/remote columns;
  it has no suite catalog or durable native suite placement selection. Its
  preparation pane combines eval/publication/planning behind one spinner.
- Existing suite-owned objects refuse independent-agent invocation. The new
  member path must verify suite placement rather than weaken that refusal.

## One object model, with honest origins

| View origin | What is known | What must not be inferred |
|---|---|---|
| Workspace | Editable source, declared members/cases, source fingerprint | Published identity, passed evaluation or placement |
| Packaged suite | Validated graph and exact OCI manifest identity | Evaluation or deployment merely from registry presence |
| Verified placement | Exact suite/member, runtime, physical target and resource observations | A readable answer or current evaluation from Ready alone |
| Standalone-agent wrapper | One observed agent, runtime, cluster identity, namespace and UID | An AgentSuite artifact, ownership receipt, evaluation or published release |

The wrapper is an **in-memory presentation projection**, marked
`Standalone · not extracted`. It has no fabricated suite digest or persisted
ownership annotation. Refreshing, opening details or chatting with an independent
agent must not create files or convert it. Known suite members are grouped by
verified suite/placement identity; incomplete ownership annotations are shown as
`Suite-owned · identity unavailable`, never demoted to standalone for extraction.

Default `create` already authors a one-member suite workspace. The new console
authoring action uses that same writer. Quickstart's generated workspace becomes
a real suite entry identified by its returned source/artifact identities.
Agents sharing a name or Provider are not implicitly members of the same suite.

Selecting a suite exposes member actions. Selecting a member retains suite
context, target and evidence. No fixed global "current suite" is required;
explicit selection belongs to the console session or invocation.

## View and navigation

```text
SUITES                         SUPPORT ASSISTANT
▾ support-assistant            Source: ./support · modified since evaluation
  └ assistant · Orka           Artifact: sha256:… · published
▾ standalone: helper           Evidence: previous revision · stale for source
  └ helper · not extracted
                               DESTINATIONS
                               dev  · evaluated · ready · answered
                               prod · previous digest · observation 12s ago

Source | Members | Cases | Artifacts | Destinations | Activity

Create member   Prove   Publish   Lift   Chat member   Extract AgentSuite
```

The left pane is local suites/workspaces and the right pane remote suites; each
shows member children and tabbed details. Standalone extraction inputs are marked
as unowned, not deployed suites. Opening the console can show local workspaces;
cluster reads use the resolved two context selections. Do not recursively scan arbitrary
disk trees or every kubeconfig cluster to populate a catalog.

Tabs progressively disclose:

1. **Source:** instructions, exact model/profile, source changes and validation.
2. **Members:** stable member IDs, dependencies and capability availability.
3. **Cases:** authored inputs/assertions, per-case execution state and verdicts.
4. **Artifacts:** suite graph versus OCI manifest identity, publication location,
   required images or `No member image required` for native profiles.
5. **Destinations:** named selection, resolved context/namespace, runtime/binding
   expectation, observed placement and source-only promotion evidence.
6. **Activity:** current operation stages and member execution lineage. Historical
   runs retain their original identity; never attach them to today's same name.

Keep arrow/Vim navigation, Enter, Escape, help, plain-terminal fallbacks and
terminal restoration. Filter actions by capability and selection. Replace dead
tool/inference editing actions with source navigation and precise unsupported
reasons. All product views show actual authored or observed state; test fixtures
are not exposed as a demo mode.

## Keep agent actions; separate suite operations

No existing command is renamed. These extensions and UI mappings use existing
operations; artifact-reference show and broader profile support remain future:

| Intent | CLI / proposed addition | TUI action |
|---|---|---|
| Create one member | Existing root `create NAME --workspace DIR …` | Create agent in a one-member suite workspace |
| Inspect independent agent | Existing `orka list/show` | Show live agent |
| Run/chat independent agent | Existing root `run --agent NAME` / `chat NAME` | Run / Chat under standalone wrapper |
| Extract live source | **New:** `suite extract NAME --runtime orka --context CTX --namespace NS --workspace NEW_DIR --name NEW_MEMBER [--plan]` | Extract AgentSuite |
| Review suite content | Existing `suite validate/diff`; **new:** `suite show WORKSPACE` (digest-reference show deferred) | Source / Members / Cases |
| Evaluate/publish | Existing root `eval` and `push` | Prove / Publish |
| Place exact suite | Existing digest `lift` | Lift selected artifact |
| Observe suite placement | **New:** `suite status --deployment ID` | Destination status |
| Invoke suite member | **Extend:** `run/chat --deployment ID --member MEMBER` | Run / Chat selected member |

`--deployment` names verified placement identity, not a kube context. Root
`status` remains its existing runtime report. A proposed member selector is
exclusive with independent-agent selection; resolve one member automatically
only if there is exactly one eligible member. No session silently switches
identity when inventory refreshes. Unsupported native/runtime paths refuse.

"Keep agent commands" means keeping these agent-level capabilities and their
current CLI spellings while centering the suite in the UI. The previously
removed noun group is not restored by this plan. Literal agent-prefixed aliases,
if desired, need a separate #301 decision and must delegate to the same handlers.
Slash commands mirror implemented equivalents (`/create`, `/run`, `/chat`,
`/suite show`, `/suite extract`, `/eval`, `/push`, `/lift`); commands for future
capabilities are not registered or advertised early.

## Extract AgentSuite: read cluster, write new source

### Operation contract

1. Select exactly one **non-suite-owned native Orka Agent** by context,
   namespace and name; retain cluster identity, UID and ResourceVersion.
2. Read the Agent and only the dependencies needed to resolve supported source:
   referenced Provider and, for supported prompt references, its ConfigMap key.
   Use caller permissions and explicit names/namespaces, bounded reads and strict
   resource decoding. No Secret-value read, discovery by content, or export of
   status/transcripts/session memory.
3. Produce a typed extraction plan before filesystem mutation: supported fields,
   resolved exact model, instructions source, unsupported field paths, new member
   name and exact destination directory. Do not echo arbitrary live values in
   diagnostic errors. Instructions copied into source are future published content.
4. Refuse unsupported behavior rather than silently drop it. Re-read source and
   dependencies before committing the result; changed UID/ResourceVersion or
   dependency identity requires a new plan. These brackets detect observed changes,
   not an atomic distributed snapshot or future drift absence.
5. Strictly validate and prepare the generated workspace offline, then publish
   its files exclusively to a previously absent destination, workspace marker last.
   An incomplete directory can remain after a filesystem error. Fail on existing roots,
   symlink escapes or collisions; no merge/overwrite into another user's source.
   Reuse existing workspace serialization/validation rather than write ad hoc YAML.
6. Return new source identity plus a field-level extraction report. Success says
   `Extracted source · original unchanged · unevaluated · not deployed`.

`--plan` performs only bounded cluster reads: no workspace/cache/state files,
admission, runtime installs, model calls or cluster writes. Apply rechecks the
retained plan and writes local source only. Noninteractive extraction explicitly
names the new path/member; TUI first shows and then saves the same plan. Neither
path changes source labels, annotations, owner references, Provider or Secret.

Use a distinct new member name by default, for example `helper-copy`; require
the explicit CLI `--name` to prevent accidentally treating the original as the
new evaluation target. Before evaluation/Lift, existing ownership/collision checks
still apply. Extraction does not grant adopt/update permission for the original,
even when its spec matches. The original remains separately visible after extract.

### First supported mapping

| Live input | Extraction rule |
|---|---|
| Native Agent identity | New member name, original name only in private provenance |
| System instructions | Exact inline text, or materialized supported ConfigMap key with digest and recheck |
| Model | Effective explicit model; resolve supported Provider default if the Agent has no override; never guess |
| Provider | Require an accurately representable openai-type source for the initial native profile |
| Endpoint/auth binding | Do not put endpoint/Secret bindings into portable source; select destination bindings separately |
| Description | Copy only a supported declared field, not arbitrary annotations/status |
| Tools, skills, delegation, memory settings, tuning or other behavior | Refuse unless the workspace profile explicitly represents them; list unsupported paths |
| External runtime, unknown CRD fields, unresolved prompt | Inspection remains available; extraction refuses with a precise reason |
| Any suite ownership annotation | Refuse extraction; use verified package retrieval when available |

Do not equate a public AgentSuite graph's ability to represent Tools/multiple
members with current native execution support. Tool-free extraction is the first
qualified slice, not a claim to clone any Agent faithfully. No lossy `--force`
option is part of this scope.

Keep any retained source observation report private outside authored/published content:
cluster identity, original UID/version, observed dependency identities and mapping
outcome are provenance only, not a placement or evaluation receipt. Do not export
kubeconfig, credential bytes, arbitrary labels/annotations or automatic `kmx.yaml`
destinations. Source/config text passes existing credential-shape validation;
matching or unreadable input refuses before authored output is committed.

Extraction cannot infer correct evaluation cases from an existing deployment.
Any scaffolder-provided cases must be explicitly shown as examples and require
author review/editing; do not fabricate historical pass results or silently
execute examples. Full ordered evaluation of the new exact package is still
required before evidence-checked publication and Lift.

## Incremental visuals from operation events

Replace the combined preparation spinner with shared typed stage/case/resource
events. CLI and TUI consume the same operation results; terminal text is not an
API. Prefer a small internal event sink over a new public lifecycle SDK.

```text
✓ Read source             current fingerprint
✓ Prepare suite           exact artifact sha256:…
✓ Case: greeting          passed  1.2s
● Case: useful-answer     judging  2/2 samples
○ Export evidence         full pass required
○ Publish                 selected registry/layout
○ Review Lift             prod · source evidence only
○ Apply / Verify          pending explicit review
○ Ask member              separate fresh execution
```

Each event has operation/selection identity, monotonic sequence, stage, member
and case/resource ID when relevant, elapsed time and typed outcome. Events carry
safe metadata, never Secret values, judge reasoning or raw subprocess payloads.
Prompt/answer content stays in the deliberately selected source/case/chat views.
Surface `pending`, `running`, `passed`, `failed`, `unknown`, `cancelled`,
`not attempted` and `partial` distinctly; use counts only for actual known units.
An interrupted event stream yields unavailable observation, not a failed or
successful remote operation. Cancellation does not claim resource rollback.

Bound queues, snapshots, history and watches. Switching selections or closing
panes cancels their observers and rejects old operation-generation events. A
hidden/detached view cannot authorize effects. Do not block resource-outcome
recording on rendering; preserve acknowledged partial outcomes if output fails.
Source edits invalidate candidate/evidence applicability visibly, while retaining
past evidence as history. Source pass, placement Ready and fresh answer have
separate badges. Never paint the whole journey green on placement alone.

## Full demo sequence and boundaries

1. Open the suite console; author a one-member workspace, or select a standalone
   Agent and **Extract AgentSuite** to a fresh member/workspace.
2. Inspect/edit instructions and author meaningful cases. Review source changes.
3. Select `dev` from named destinations; run full evaluation with per-case progress.
   Demonstrate a failing case: later sends stop and no passing evidence exports.
4. Correct source/cases, prepare a new exact identity and evaluate successfully.
5. Publish that exact artifact; show reference/digest and evidence match. Native
   profiles skip image construction; image profiles use #448/#449 when qualified.
6. Select `prod`, review source-only promotion and the GET-only plan, then approve
   the common single-use Apply/Verify. Registry transfer is explicit; Lift remains
   deploy-only. A TUI can sequence operations but cannot hide evaluation inside Lift.
7. Invoke the selected placed member through a suite-aware operation and show a
   fresh answer. Preserve the independent original Agent and its access path.
8. Restart the console, rediscover retained placement input, revalidate it and
   show status/chat for that identity. This is rediscovery, not automatic retry.

No cluster/resource-group deletion is a suite/member action or the inverse of
Lift. Retained retirement versus physical reclamation remains #347; no implicit
candidate cleanup is added here. Explicit user-owned fixtures provide demo
cleanup separately. Keep the existing exact-runtime restrictions: kagent stays
out of composed candidate flows until independently qualified, and AgentSessions
development evaluation does not become a deployment/chat capability.

## Delivery slices

| Slice | Shared operation and UI work | Acceptance |
|---|---|---|
| A. Suite projection | Local/remote suite/member/standalone view model, source selection, capability-aware actions and bounded live polling | Offline workspace visible; default single-member suite works; standalone browsing writes nothing; no fabricated digest/ownership; dead editor actions removed |
| B. Extract | Typed observation/mapping/plan/write flow, proposed CLI and TUI action, fresh output writer | Exact supported fields round-trip; unsupported behavior refuses; original/dependencies unchanged; no Secret values read; no model/admission/write to cluster; collision/drift/cancel tests |
| C. Progress | Bounded typed stage/case events from existing prepare/eval/push/Lift operations | Both CLI and TUI reflect real phases; no fabricated percentages; fail-fast and privacy preserved; resize/cancel/stale-event tests |
| D. Suite actions | Offline creation/editing, source/case review, named destination resolution and registry publication; suite-based Lift review | Full local-to-remote path within one console using shared handlers; source/evidence/destination changes invalidate previous review |
| E. Placement and member actions | Retained native placement inputs, revalidated observation/run/chat; integrate image equivalents from #449 | Fresh-process rediscovery; correct UID/runtime/artifact/member association; suite-owned independent invocation still refuses; no raw-name fallback |
| F. Qualification and docs | Exact-head PTY and live two-destination demo, updated current console guide/help/conventions/AGENTS.md | Complete scenario above, original retained, failure control, interruption/partial readback, no unintended cleanup; reproducible scripted evidence |

A can precede new lifecycle APIs. B and C are bounded first implementation PRs.
D/E complete the native demo; image end-to-end additionally depends on #448/#449.
Multi-member mutation/evaluation, ToolProvider execution and graph visualization
can follow, with real supported edges rather than decorative topology. A suite
with unsupported members remains inspectable with accurate action refusals.

## Code touchpoints and coordination

- Console: `internal/kmx/app/agent_tui*.go`, `suite_workspace_review.go` and
  `quickstart_wizard_suite.go`; reuse source review and consent instead of a
  parallel state machine. The old console guide is labeled historical and needs
  a new current section only as each capability ships.
- Source: `internal/kmx/agentsuite/workspace`; extract into its current closed
  source format, not removed user bundle readers or public runtime fields.
- Cluster observation: runtime-qualified reads adjacent to `agent_show.go` and
  `suite_ownership.go`; inspect actual pinned CRD semantics for the mapping.
- Lifecycle: `suite_workspace_evaluate.go`, `suite_native_evaluate.go`,
  `suite_judge.go`, `quickstart_suite.go`, `release_lift.go` and shared transfer.
  Quickstart convenience functions are not a second remote deployment engine.
- Commands: `cmd/kmx/suite_commands.go`, root run/chat and destination selection;
  new modes require strict mutually exclusive selectors and inert help/completion.

| Owner | Coordinate, do not duplicate |
|---|---|
| [#447](https://github.com/kaimahi-agents/kaimahi/issues/447), [#448](https://github.com/kaimahi-agents/kaimahi/pull/448), [#449](https://github.com/kaimahi-agents/kaimahi/pull/449) | Image release/evidence, shared console extension and retained image placement; refresh tracker status from current PR heads |
| [#344](https://github.com/kaimahi-agents/kaimahi/issues/344), [#346](https://github.com/kaimahi-agents/kaimahi/issues/346), [#348](https://github.com/kaimahi-agents/kaimahi/issues/348) | Native placement identity, binding rehydration, cross-process observation and operation recovery; a display cache grants no mutation authority |
| [#295](https://github.com/kaimahi-agents/kaimahi/issues/295) | Bounded live refresh and stale-event rejection |
| [#276](https://github.com/kaimahi-agents/kaimahi/issues/276), [#280](https://github.com/kaimahi-agents/kaimahi/issues/280), [#282](https://github.com/kaimahi-agents/kaimahi/issues/282), [#283](https://github.com/kaimahi-agents/kaimahi/issues/283) | Shared operations, UI-free authoring, runtime results and UI extraction; no second package hierarchy required for the first slice |
| [#301](https://github.com/kaimahi-agents/kaimahi/issues/301), [#351](https://github.com/kaimahi-agents/kaimahi/issues/351) | New extraction/member selectors and suite-oriented language |
| [#350](https://github.com/kaimahi-agents/kaimahi/issues/350), [#493](https://github.com/kaimahi-agents/kaimahi/issues/493), [#497](https://github.com/kaimahi-agents/kaimahi/pull/497) | Case identity/location and evaluation binding; extraction does not define a rival eval format |

## Decisions to close before their implementation slices

- Confirm `suite extract` as the public spelling and the distinction between
  a standalone presentation wrapper and a real authored/packaged suite.
- Specify the exact Orka field coverage against pinned CRDs, including defaults
  and prompt references. Do not ship an approximate copier as faithful extraction.
- Choose placement-store semantics with #346 and reconcile #449's experimental
  retained IDs before advertising native cross-process `--deployment` support.
- Choose initial suite catalog roots and private persistence of locations. Keep
  source, operational destinations, evidence and private observations separate.
- Preserve existing root agent verbs; decide literal agent-prefixed syntax only
  if requested explicitly, through #301 rather than restoring removed readers.

This branch implements the bounded native slice described in "Test this branch".
It preserves current ownership checks and does not claim full multi-member/runtime
parity or live end-to-end qualification. Use the delivery matrix to split later
PRs and qualify the complete demonstration.
