# Kaimahi Repository Front Door Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorder the main repository's public front door around the working product, add repository-specific contribution guidance, and finish the relevant GitHub metadata and security settings.

**Architecture:** The root README becomes a concise router: identity, product definition, three governance outcomes, architecture, working quickstart, status, then detailed references. Existing capability docs remain the source of detailed truth. Automated checks protect heading order, product copy, asset links, and the rule that the proposed CLI cannot outrank the working Makefile path.

**Tech Stack:** Markdown, Python 3 standard library, existing documentation link checker, GitHub repository settings/rulesets.

**Spec:** `docs/superpowers/specs/2026-09-01-organization-and-brand-design.md`

## Global Constraints

- Lead with `Governance for AI agents running on Kubernetes.`
- The working `make up && make chat` path must appear before the proposed `npx kaimahi create` path.
- Preserve built, demonstrated-once, schema-valid, proposed, and unbuilt distinctions.
- Preserve existing capability docs rather than copying their full details into the README.
- Use the approved hero and architecture files from the brand plan.
- Do not add `GOVERNANCE.md` or `CODE_OF_CONDUCT.md`.
- Audit the existing `protect-main` ruleset; do not create a competing protection mechanism blindly.

---

## File map

| File | Responsibility |
|---|---|
| `README.md` | Project identity, outcomes, architecture, working quickstart, truthful status, routes |
| `CONTRIBUTING.md` | Repository-specific development and verification instructions |
| `scripts/check-readme-front-door.py` | Protects public hierarchy and stable copy |
| `.github/workflows/ci.yml` | Runs README hierarchy validation in hygiene |

### Task 1: Add a failing README hierarchy check

**Files:**
- Create: `scripts/check-readme-front-door.py`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `README.md` and canonical asset paths.
- Produces: a fast CI failure when public hierarchy regresses.

- [ ] **Step 1: Create the complete checker**

```python
#!/usr/bin/env python3
from pathlib import Path

readme = Path(__file__).resolve().parents[1] / "README.md"
text = readme.read_text()

# Required in this order: identity, the three governance outcomes,
# architecture, then the working quickstart before status.
order = [
    "brand/hero.png",
    "Governance for AI agents running on Kubernetes.",
    "### Control model spend",
    "### Constrain tool calls",
    "### Approve consequential actions",
    "docs/assets/architecture.svg",
    "## Quickstart",
    "make up",
    "make chat",
    "## Status",
]

missing = [item for item in order if item not in text]
if missing:
    raise SystemExit("README front door missing: " + ", ".join(missing))

positions = [text.index(item) for item in order]
if positions != sorted(positions):
    raise SystemExit("README front-door sections are out of order")

# The proposed CLI may not appear anywhere before the end of the working
# quickstart, including between `make up` and `make chat`.
proposed_cli = text.find("npx kaimahi create")
if proposed_cli != -1 and proposed_cli < text.index("make chat"):
    raise SystemExit("proposed CLI appears before the working quickstart")

print("README front door: identity, outcomes, architecture, quickstart, and status order valid")
```

- [ ] **Step 2: Run and confirm failure before rewriting**

Run: `python3 scripts/check-readme-front-door.py`

Expected: exit `1`, initially reporting missing `brand/hero.png` or the product
line/front-door headings.

- [ ] **Step 3: Add the check to CI**

Immediately after `Doc links resolve` in `.github/workflows/ci.yml`, add:

```yaml
      - name: README front door is ordered around working capability
        run: python3 scripts/check-readme-front-door.py
```

If the brand plan already added its check at this location, keep both adjacent.

- [ ] **Step 4: Commit the failing guard**

```bash
git add scripts/check-readme-front-door.py .github/workflows/ci.yml
git commit -m "test: protect README front-door hierarchy"
```

### Task 2: Rework the README opening

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: `brand/hero.png`, `docs/assets/architecture.svg`, and existing status/capability docs.
- Produces: the public project narrative protected by Task 1.

- [ ] **Step 1: Replace the current preamble through the existing Quickstart heading**

Use this complete opening, then retain and reconcile the existing prerequisites,
commands, status table, and detailed sections beneath it:

````markdown
<p align="center">
  <img src="brand/hero.png"
       alt="Kaimahi night worker guarding paths for AI agents"
       width="100%">
</p>

# Kaimahi

> **Incubation project.** Kaimahi is built in public. The README and
> documentation label capabilities as running in CI, demonstrated once,
> schema-valid only, proposed, or unbuilt.

**Governance for AI agents running on Kubernetes.**

Kaimahi builds on [kagent](https://kagent.dev) rather than replacing it. It adds
controls at the model and MCP boundaries for consequential agent work.

### Control model spend

Meter model calls, fail closed on monthly budgets, write every request to a
ledger, and keep real provider credentials away from agent pods.

### Constrain tool calls

Route MCP traffic through an enforcing gateway with explicit upstreams,
per-credential tool allowlists, and an audit trail that includes denials.

### Approve consequential actions

Turn a denied model or tool action into a pending request. Human approval issues
a grant limited by expiry, use count, or both. The exception lapses when its
limit is reached.

<p align="center">
  <img src="docs/assets/architecture.svg"
       alt="A Kubernetes agent routes model calls through the Kaimahi LLM proxy and tool calls through its MCP gateway; bounded approvals can widen either path temporarily">
</p>

Governance is opt-in per agent. The documentation identifies ungoverned paths
and current limitations.

## Quickstart

The working path today:

```bash
make up     # kind cluster + local model + kagent + agents (~5–10 minutes)
make chat   # talk to the default agent
```

The default path needs no API key. It uses an in-cluster Ollama model for a real
agent conversation. Continue with the [getting-started guide](docs/getting-started.md)
or choose a capability from the [documentation index](docs/README.md).
````

- [ ] **Step 2: Move the proposed CLI section below working capability**

Retain the `npx kaimahi create` example and its explicit “proposed, not built”
warning, but place it after Quickstart and current status or under a clearly
labeled `## Proposed CLI direction` heading. Do not include it in the first
screen above Quickstart.

- [ ] **Step 3: Deduplicate without deleting factual caveats**

For each existing section, keep one of these outcomes:

- retain if it is the shortest working reference;
- shorten to a link when the same detail is authoritative in `docs/`; or
- keep verbatim when it is a maturity/security limitation not stated elsewhere.

Do not delete the target-cluster confirmation behavior, live-verification
markers, governed-versus-ungoverned warning, AKS demonstrated-once statement,
or capability status table.

- [ ] **Step 4: Run the focused checks**

```bash
python3 scripts/check-readme-front-door.py
python3 scripts/check-doc-links.py
git diff --check
```

Expected: all exit `0`.

- [ ] **Step 5: Render and inspect the README**

Preview Markdown at desktop and narrow widths. Confirm the hero and architecture
images load, headings do not create a wall of equal-weight text, and Quickstart
appears before the proposed CLI.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: lead README with Kaimahi governance outcomes"
```

### Task 3: Add repository-specific contribution guidance

**Files:**
- Create: `CONTRIBUTING.md`

**Interfaces:**
- Consumes: organization default and existing Make/CI commands.
- Produces: local setup and verification detail that should not live in the organization default.

- [ ] **Step 1: Confirm the organization guide the local guide links to is published**

Run:

```bash
curl -fsSI https://github.com/kaimahi-agents/.github/blob/main/CONTRIBUTING.md
```

Expected: HTTP `200`. The organization community plan creates that file;
if it is still unmerged, land it first rather than publishing a dead link.

- [ ] **Step 2: Create the complete local guide**

````markdown
# Contributing to Kaimahi

Kaimahi is an incubation project. Focus contributions on fixes, documentation
corrections, tests, and small capability changes. Start with the organization
[contribution expectations](https://github.com/kaimahi-agents/.github/blob/main/CONTRIBUTING.md).

## Before building something new

Kaimahi builds on kagent, Kubernetes, and existing MCP servers. Check those
projects first. In the pull request, explain why configuration or integration
cannot provide the requested behavior.

## Local verification

Run the checks relevant to your change:

```bash
python3 scripts/check-doc-links.py
python3 scripts/check-readme-front-door.py
python3 scripts/check-brand-assets.py
bash scripts/kube-guard-test.sh
(cd plane && test -z "$(gofmt -l .)" && go vet ./... && go test ./...)
```

For cluster changes, use the documented kind path and a dedicated `KIND_CLUSTER`
name when another lane owns the shared cluster. See
[`docs/COORDINATION.md`](docs/COORDINATION.md) for the process and
[`docs/getting-started.md`](docs/getting-started.md) for prerequisites.

## Pull requests

- Keep each pull request focused on a user problem.
- List exact verification commands and results.
- Label behavior as continuously tested, demonstrated once, schema-valid,
  proposed, or unbuilt.
- Update the capability documentation and status language with the code.
- Never include API keys, tokens, private endpoints, tenant/subscription IDs,
  registry names, cluster addresses, or unsanitized user data.

Every change lands through a pull request with required checks green.
````

- [ ] **Step 3: Validate and commit**

```bash
python3 scripts/check-doc-links.py
git diff --check
git add CONTRIBUTING.md
git commit -m "docs: add repository contribution guide"
```

### Task 4: Run repository-wide verification and open the front-door PR

**Files:** None beyond prior tasks.

**Interfaces:**
- Produces: one reviewable main-repository PR based on the merged brand branch.

- [ ] **Step 1: Rebase onto the merged brand work**

The front-door branch must contain final asset paths and the validator from the
brand PR. Resolve `.github/workflows/ci.yml` by preserving both new hygiene
checks.

- [ ] **Step 2: Run fast and full relevant checks**

```bash
python3 scripts/check-brand-assets.py
python3 scripts/check-readme-front-door.py
python3 scripts/check-doc-links.py
bash scripts/check-no-azure-ids.sh
bash scripts/kube-guard-test.sh
(cd plane && test -z "$(gofmt -l .)" && go vet ./... && go test ./...)
git diff --check
```

Expected: all exit `0`.

- [ ] **Step 3: Open the PR with explicit visual evidence**

Include screenshots/previews of the README at desktop and narrow widths. State
that detailed factual content was reordered or linked, not silently discarded.
Wait for all required checks, including the existing end-to-end check.

### Task 5: Apply and verify GitHub metadata and security settings

**Files:** None.

**Interfaces:**
- Consumes: merged `brand/social-preview.png`, existing `protect-main` ruleset,
  and organization admin access.
- Produces: public repository metadata and verified settings; no source-tree changes.

- [ ] **Step 1: Record current settings before changing them**

Inspect repository description, topics, merge methods, branch-deletion setting,
security features, and the existing `protect-main` ruleset. Save the observed
values in the PR or implementation notes. Do not create a second ruleset while
`protect-main` already governs `main`.

- [ ] **Step 2: Set repository presentation**

Use this description:

```text
Governance for AI agents running on Kubernetes. Budgets, credential custody, tool controls, audit, and bounded approvals.
```

Set topics exactly to:

```text
ai-agents
kubernetes
kagent
mcp
governance
llm-security
agentic-ai
```

Upload `brand/social-preview.png` in **Settings → General → Social preview** and
pin `kaimahi` on the organization profile.

- [ ] **Step 3: Set merge behavior**

Enable squash merging and automatic deletion of merged branches. Disable merge
commits. Keep rebase merging only if current contributor workflow relies on it;
otherwise disable it for one predictable history shape.

- [ ] **Step 4: Audit `protect-main` rather than replacing it**

Verify it requires pull requests and the current required checks
`hygiene`, `go-plane`, and `e2e-hello-world`; blocks force pushes and deletion;
and requires conversation resolution. Enable stale-approval dismissal and one
approval for non-bypass contributors if absent. Preserve the existing explicit
admin bypass rather than silently changing maintainer emergency access.

- [ ] **Step 5: Enable security features that will be triaged**

Enable private vulnerability reporting, dependency graph, Dependabot alerts,
Dependabot security updates, and secret scanning/push protection where the
repository plan exposes them. Confirm the private advisory creation URL works.

Before enabling CodeQL, inspect languages and existing scanning. Add it only if
the project will triage findings and the workflow does not duplicate an
existing scanner. Record a deliberate `enabled` or `deferred with reason`
decision.

- [ ] **Step 6: Verify the public result**

Open the organization and repository logged out or in a private browser. Confirm
the avatar, pinned repo, description, topics, social preview, README images,
community links, private-report route, and ruleset behavior all match the plan.
