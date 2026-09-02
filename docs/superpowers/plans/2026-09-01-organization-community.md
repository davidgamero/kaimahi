# Kaimahi Organization Community Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `kaimahi-agents/.github` into a concise organization front door with inherited contribution, support, security, issue, and pull-request defaults.

**Architecture:** The `.github` repository owns organization-wide defaults; product-specific commands remain in `kaimahi`. The profile references canonical brand files from `kaimahi/brand/` and routes visitors to the working quickstart and capability docs. A code of conduct is deliberately deferred until a private reporting channel exists.

**Tech Stack:** GitHub profile README, GitHub community-health files, GitHub issue forms, YAML, Markdown.

**Spec:** `docs/superpowers/specs/2026-09-01-organization-and-brand-design.md`

## Global Constraints

- Primary audiences are platform engineers and open-source contributors equally.
- Product line: `Governance for AI agents running on Kubernetes.`
- State that Kaimahi is an incubation project.
- Link the working `make up && make chat` path ahead of the proposed CLI.
- Do not add `GOVERNANCE.md`.
- Do not add `CODE_OF_CONDUCT.md` until a private conduct-reporting channel exists.
- Do not duplicate product-specific setup instructions in organization defaults.

---

## File map

| File | Responsibility |
|---|---|
| `profile/README.md` | Organization identity, product definition, routes |
| `CONTRIBUTING.md` | Shared contribution expectations |
| `SECURITY.md` | Private vulnerability reporting through GitHub |
| `SUPPORT.md` | Incubation-appropriate support routing |
| `.github/ISSUE_TEMPLATE/bug.yml` | Structured, secret-safe bug reports |
| `.github/ISSUE_TEMPLATE/feature.yml` | Problem-first feature proposals |
| `.github/ISSUE_TEMPLATE/config.yml` | Blank-issue policy and support/security links |
| `PULL_REQUEST_TEMPLATE.md` | Verification and claim-quality checklist |

### Task 1: Replace the organization profile

**Files:**
- Modify: `profile/README.md`

**Interfaces:**
- Consumes: merged `kaimahi/brand/hero.png` and stable public documentation URLs.
- Produces: one-screen organization front door.

- [ ] **Step 1: Confirm the canonical hero exists on `kaimahi/main`**

Run:

```bash
curl -fsSI https://raw.githubusercontent.com/kaimahi-agents/kaimahi/main/brand/hero.png
```

Expected: HTTP `200`. Do not merge a profile containing a broken hero URL.
Also enable private vulnerability reporting on `kaimahi` and confirm
`https://github.com/kaimahi-agents/kaimahi/security/advisories/new` opens the
private report flow; the profile and shared security policy link to it.

- [ ] **Step 2: Replace `profile/README.md` with the complete profile**

```markdown
<p align="center">
  <img src="https://raw.githubusercontent.com/kaimahi-agents/kaimahi/main/brand/hero.png"
       alt="Kaimahi night worker guarding paths for AI agents"
       width="100%">
</p>

# Kaimahi

**Governance for AI agents running on Kubernetes.**

Kaimahi is an incubation project built on [kagent](https://kagent.dev). It adds
model-call budgets and metering, keeps provider credentials away from agents,
constrains and audits tool calls, and supports human approval through bounded
grants for consequential actions.

Work happens in public. Capability labels say what is built, demonstrated once,
proposed, or unbuilt.

## Start here

- **[Run the quickstart](https://github.com/kaimahi-agents/kaimahi/blob/main/docs/getting-started.md)**
- [Capability documentation](https://github.com/kaimahi-agents/kaimahi/blob/main/docs/README.md)
- [Status and limitations](https://github.com/kaimahi-agents/kaimahi#status)
- [Contribute](https://github.com/kaimahi-agents/kaimahi/blob/main/CONTRIBUTING.md)
- [Report a security issue privately](https://github.com/kaimahi-agents/kaimahi/security/advisories/new)

The name is provisional; see the
[naming record](https://github.com/kaimahi-agents/kaimahi/blob/main/docs/NAMING.md)
for its history and remaining checks.
```

- [ ] **Step 3: Check links and commit**

Run each HTTPS target through `curl -fsSI`, then:

```bash
git add profile/README.md
git commit -m "docs: establish Kaimahi organization profile"
```

### Task 2: Add shared contribution, security, and support defaults

**Files:**
- Create: `CONTRIBUTING.md`
- Create: `SECURITY.md`
- Create: `SUPPORT.md`

**Interfaces:**
- Consumes: public GitHub issue and advisory surfaces.
- Produces: inherited defaults for organization repositories without local overrides.

- [ ] **Step 1: Create `CONTRIBUTING.md`**

```markdown
# Contributing

Kaimahi is an incubation project developed in public. Useful contributions
include focused bug reports, documentation fixes, design feedback, and small,
well-tested changes.

Before opening a pull request:

1. Search existing issues and pull requests.
2. Describe the user problem before proposing a new component.
3. Check the target repository's local contributing guide for setup and tests.
4. Run every check relevant to the files you changed.
5. Distinguish what you verified directly from what you inferred.

Kaimahi builds on existing agent and Kubernetes projects. Proposals for new
components should explain why upstream capabilities, configuration, or
integration are insufficient.

Never include credentials, tokens, private cluster names, tenant identifiers,
or unsanitized logs in an issue or pull request.
```

- [ ] **Step 2: Create `SECURITY.md`**

```markdown
# Security policy

## Reporting a vulnerability

Do not report suspected vulnerabilities or exploitable details publicly.

Use the affected repository's **Security** tab and choose **Report a
vulnerability** to open a private GitHub security advisory. For Kaimahi, use:

https://github.com/kaimahi-agents/kaimahi/security/advisories/new

Include the affected commit or version, deployment assumptions, reproduction
steps, impact, and any suggested mitigation. Remove credentials, tenant
identifiers, cluster addresses, and user data.

Kaimahi is an incubation project with no formal support window or response-time
SLA. Maintainers will acknowledge actionable reports and coordinate disclosure
through the private advisory.
```

- [ ] **Step 3: Create `SUPPORT.md`**

```markdown
# Support

Kaimahi is an incubation project without production support.

- Use the target repository's bug form for reproducible defects.
- Use the feature form for a concrete user problem or missing capability.
- Start with the [documentation index](https://github.com/kaimahi-agents/kaimahi/blob/main/docs/README.md)
  and [FAQ](https://github.com/kaimahi-agents/kaimahi/blob/main/docs/FAQ.md)
  for setup and usage questions.
- [Report vulnerabilities privately](https://github.com/kaimahi-agents/kaimahi/security/advisories/new),
  not in a public issue.

Sanitize logs before sharing them. Do not post API keys, tokens, tenant or
subscription identifiers, private registry names, cluster addresses, or user
data.
```

- [ ] **Step 4: Verify and commit**

Run `git diff --check` and verify every absolute link with `curl -fsSI`.

```bash
git add CONTRIBUTING.md SECURITY.md SUPPORT.md
git commit -m "docs: add organization community defaults"
```

### Task 3: Add issue forms

**Files:**
- Create: `.github/ISSUE_TEMPLATE/bug.yml`
- Create: `.github/ISSUE_TEMPLATE/feature.yml`
- Create: `.github/ISSUE_TEMPLATE/config.yml`

**Interfaces:**
- Produces: valid GitHub issue-form YAML with secret-safety prompts.

- [ ] **Step 1: Create `.github/ISSUE_TEMPLATE/bug.yml`**

```yaml
name: Bug report
description: Report a reproducible problem without secrets
title: "[Bug]: "
labels: [bug]
body:
  - type: markdown
    attributes:
      value: |
        Sanitize logs before attaching them.
        Use private vulnerability reporting for suspected vulnerabilities.
  - type: input
    id: version
    attributes:
      label: Commit or version
      placeholder: main at abc1234
    validations:
      required: true
  - type: input
    id: kubernetes
    attributes:
      label: Kubernetes distribution and version
      placeholder: kind v0.33.0 / Kubernetes v1.33.x
    validations:
      required: true
  - type: dropdown
    id: target
    attributes:
      label: Deployment target
      options:
        - kind
        - AKS
        - Other conformant Kubernetes cluster
    validations:
      required: true
  - type: textarea
    id: reproduction
    attributes:
      label: Reproduction
      description: Give the shortest exact sequence that reproduces the problem.
    validations:
      required: true
  - type: textarea
    id: expected
    attributes:
      label: Expected result
    validations:
      required: true
  - type: textarea
    id: actual
    attributes:
      label: Actual result
    validations:
      required: true
  - type: textarea
    id: logs
    attributes:
      label: Sanitized logs
      description: Remove credentials, private endpoints, tenant/subscription IDs, and user data.
      render: shell
    validations:
      required: true
  - type: checkboxes
    id: safety
    attributes:
      label: Safety checks
      options:
        - label: I removed credentials, private endpoints, tenant or subscription IDs, and user data.
          required: true
        - label: I searched existing issues and reproduced this on a current commit or release.
          required: true
```

- [ ] **Step 2: Create `.github/ISSUE_TEMPLATE/feature.yml`**

```yaml
name: Feature request
description: Describe the user problem before proposing a component
title: "[Feature]: "
labels: [enhancement]
body:
  - type: textarea
    id: problem
    attributes:
      label: User problem
      description: Who is blocked, and what can they not do safely or simply today?
    validations:
      required: true
  - type: textarea
    id: outcome
    attributes:
      label: Desired outcome
      description: Define observable success without prescribing an implementation.
    validations:
      required: true
  - type: textarea
    id: alternatives
    attributes:
      label: Alternatives and current workaround
      description: Include relevant kagent, Kubernetes, or MCP capabilities you considered.
    validations:
      required: true
  - type: textarea
    id: governance
    attributes:
      label: Governance impact
      description: Note effects on credentials, spend, tools, approvals, network boundaries, audit, or blast radius.
    validations:
      required: true
  - type: checkboxes
    id: survey
    attributes:
      label: Existing capability check
      options:
        - label: I checked whether kagent, Kubernetes, or an MCP component already provides this capability.
          required: true
```

- [ ] **Step 3: Create `.github/ISSUE_TEMPLATE/config.yml`**

```yaml
blank_issues_enabled: false
contact_links:
  - name: Documentation and FAQ
    url: https://github.com/kaimahi-agents/kaimahi/blob/main/docs/README.md
    about: Setup, capability documentation, and known limitations.
  - name: Private security report
    url: https://github.com/kaimahi-agents/kaimahi/security/advisories/new
    about: Report suspected vulnerabilities privately, not in a public issue.
```

- [ ] **Step 4: Validate YAML and required fields**

Run:

```bash
ruby -e 'require "yaml"; Dir[".github/ISSUE_TEMPLATE/*.yml"].each { |f| YAML.load_file(f); puts f }'
rg -n 'required: true|private|credentials|user problem' .github/ISSUE_TEMPLATE
git diff --check
```

Expected: Ruby prints all three files and exits `0`; grep finds the required
guardrails.

The forms apply the `bug` and `enhancement` labels, which GitHub creates by
default in every new repository. Confirm they still exist wherever the forms
will be used:

```bash
gh api repos/kaimahi-agents/kaimahi/labels --jq '.[].name' | grep -x -e bug -e enhancement
```

Expected: both names print. Recreate a missing label before relying on the form.

- [ ] **Step 5: Commit**

```bash
git add .github/ISSUE_TEMPLATE
git commit -m "chore: add organization issue forms"
```

### Task 4: Add the shared pull-request template

**Files:**
- Create: `PULL_REQUEST_TEMPLATE.md`

**Interfaces:**
- Produces: one verification-focused default inherited by repositories without a local template.

- [ ] **Step 1: Create the complete template**

```markdown
## Problem

What problem does this change solve for users or maintainers?

## Approach

What changed? Why is this the smallest sufficient approach?

## Verification

List exact commands or manual checks and their results.

- [ ] Relevant automated tests pass.
- [ ] Documentation and examples match the implemented behavior.
- [ ] Logs, screenshots, and fixtures contain no credentials or private identifiers.

## Security and governance

Note effects on credential custody, spend, tool access, approvals, network
boundaries, audit, or blast radius. Write `No change` if there are none.

## Claim quality

Label each new capability claim as continuously tested, verified manually,
demonstrated once, schema-valid only, proposed, or unbuilt.
```

- [ ] **Step 2: Verify and commit**

```bash
git diff --check
git add PULL_REQUEST_TEMPLATE.md
git commit -m "chore: add pull request template"
```

### Task 5: Verify GitHub inheritance after merge

**Files:** None.

**Interfaces:**
- Consumes: merged `.github` default-branch files.
- Produces: evidence that GitHub recognizes the files rather than merely storing them.

- [ ] **Step 1: Open the `.github` repository PR and wait for review**

The PR must explicitly state that `GOVERNANCE.md` and `CODE_OF_CONDUCT.md` are
deferred. Merge only after the brand PR has made `brand/hero.png` public.

- [ ] **Step 2: Inspect the rendered organization page**

Verify the hero loads, the profile fits the intended hierarchy, and the stale
`GUIDE.md` link is gone.

- [ ] **Step 3: Inspect the community-health surface**

In `kaimahi`, open **Insights → Community Standards** and verify GitHub detects
the inherited contributing, security, support, issue-template, and PR-template
files. Confirm no Code of Conduct or governance file is claimed.

- [ ] **Step 4: Exercise forms without submitting**

Start a new issue and PR. Confirm both issue forms render, required controls are
enforced, the security link routes privately, and the PR body is prefilled.
