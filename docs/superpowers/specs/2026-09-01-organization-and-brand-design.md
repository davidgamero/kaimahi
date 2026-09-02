# Kaimahi organization and brand design

**Status:** Approved in conversation; implementation pending written-spec review
**Date:** 2026-09-01

## Purpose

Give the `kaimahi-agents` GitHub organization and its primary repository a
coherent public front door for two equally important audiences:

- platform engineers evaluating whether Kaimahi solves a real governance
  problem; and
- open-source contributors deciding whether they understand and trust the
  project enough to participate.

The work should improve first impressions without overstating the maturity of
an incubation project or splitting a still-evolving system across premature
repository boundaries.

## Goals

1. Establish a recognizable identity based on the original product idea: a
   quiet helper that fixes things overnight.
2. Make the organization profile explain the product and route a visitor in
   one screen.
3. Make the main repository's first screen communicate the problem, working
   capabilities, and shortest successful path before presenting detailed
   reference material.
4. Add the minimum shared contribution, support, and security conventions
   expected of a public open-source organization.
5. Preserve Kaimahi's existing standard of distinguishing what runs today,
   what has been demonstrated once, and what is only proposed.

## Non-goals

- Creating a governance model or `GOVERNANCE.md` at this stage.
- Publishing a code of conduct before a private conduct-reporting channel is
  available.
- Splitting the CLI, governance plane, manifests, or documentation into
  separate repositories.
- Creating a marketing or documentation website.
- Rebranding Kaimahi as production-ready.
- Using Māori-inspired patterns, characters, clothing, or ornamentation as a
  visual shortcut for the name.
- Rewriting the technical documentation or changing product behavior.

## Brand direction

### Concept

Kaimahi's identity is an original **night worker**: a small, capable workshop
sprite that quietly keeps systems healthy while people sleep. It may carry a
lantern, wrench, or compact tool satchel. The character should feel competent,
protective, and slightly magical, not cute for cuteness's sake.

The character is not a traditional Christmas elf and should avoid pointed
ears, holiday clothing, or familiar franchise silhouettes. It is also not
visually coded as Māori. The name uses the te reo Māori word for *worker*; the
visual story comes from the project's independent “overnight helper” origin.

### Visual language

- Deep navy: nighttime, reliability, infrastructure.
- Moonlit blue or teal: paths, system state, controlled flow.
- Warm lantern amber: human judgment, help, and approved action.
- Soft off-white: moonlight and readable contrast.
- Restrained glow: used to establish warmth and focus, never as decorative
  noise around technical diagrams.

The overall tone is **quietly competent, protective, and slightly magical**.

### Product line

Use this clear definition near the Kaimahi name:

> Governance for AI agents running on Kubernetes.

Longer copy may name kagent, budgets, credential custody, tool allowlists,
audit, and bounded approvals. The short line should remain stable and avoid
packing the complete capability list into the wordmark or avatar.

## Asset system

Canonical brand assets live in the `kaimahi` repository under `brand/`. The
organization profile may reference those assets with absolute GitHub URLs, and
the organization avatar is uploaded manually from the canonical source.

```text
brand/
├── README.md
├── mascot.png
├── mark.svg
├── mark.png
├── wordmark.svg
├── hero.png
└── social-preview.png
```

`brand/README.md` records the concept, palette values, file purposes, minimum
sizes, background guidance, and the image-generation/editing provenance needed
to reproduce or revise the artwork.

### 1. Organization avatar

- Source: `brand/mark.svg` and a high-resolution `brand/mark.png` export.
- Composition: the night worker's head or silhouette combined with a crescent
  or warm lantern.
- Requirement: recognizable at approximately 40 px; no wordmark or small
  tools; strong silhouette; square crop with safe margin.
- The current crescent-and-gear image is a useful palette/composition reference,
  but the final mark should introduce the distinctive worker identity.

### 2. Repository hero

- Source: `brand/hero.png`.
- Wide composition for the top of the main README and organization profile.
- Show the worker tending a small number of guarded, luminous paths that imply
  model and tool traffic without becoming an architecture diagram.
- Reserve quiet space for the Kaimahi wordmark and product line.
- Avoid embedding detailed capability claims in the bitmap; surrounding
  Markdown remains accessible, searchable, and easy to update.

### 3. GitHub social preview

- Source: `brand/social-preview.png`.
- Exact canvas: 1280 × 640 px.
- Derived from the hero's visual language but composed specifically for link
  previews, with large Kaimahi name, product line, and a compact mascot.
- Verify important content inside a central safe area and at small preview
  sizes.

### 4. Architecture diagram

- Source: `docs/assets/architecture.svg`, with editable source beside it if a
  separate source format is used.
- Precise vector artwork, not generative illustration.
- Show Kubernetes-hosted agents entering Kaimahi through two existing seams:
  model traffic through the governed LLM proxy and tool traffic through the
  enforcing MCP gateway. Show budget/ledger, credential custody, allowlist,
  audit, and bounded approvals as controls attached to the correct seam.
- Distinguish governed paths from explicitly ungoverned alternatives without
  implying hostile-pod containment beyond the documented NetworkPolicy scope.
- Use the brand palette, direct labels, and accessible text. It must remain
  readable on GitHub in light and dark appearances.

## GitHub organization structure

Keep the organization intentionally small:

| Repository | Purpose |
|---|---|
| `kaimahi` | Product monorepo: governance plane, manifests, CLI work, tests, documentation, and canonical brand assets |
| `.github` | Organization profile and inherited community-health defaults |

Do not create a site repository until there is a real website with content that
cannot be served well from the repository and docs.

### `.github` repository

```text
.github/
├── profile/
│   └── README.md
├── CONTRIBUTING.md
├── SECURITY.md
├── SUPPORT.md
├── .github/
│   └── ISSUE_TEMPLATE/
│       ├── bug.yml
│       ├── feature.yml
│       └── config.yml
└── PULL_REQUEST_TEMPLATE.md
```

These root-level community-health files act as organization defaults where a
repository does not provide a more specific version.

### Organization profile content

The profile README should fit its core message into one screen:

1. Hero or compact brand lockup.
2. “Governance for AI agents running on Kubernetes.”
3. One short paragraph naming budgets, credential custody, governed tool calls,
   audit, and bounded human approvals.
4. One primary link to the two-command quickstart.
5. Secondary links to documentation, project status, contributing, and
   security.
6. An explicit incubation label.

The current profile copy is a sound factual base. Replace the stale `GUIDE.md`
link with the current documentation index and improve hierarchy rather than
expanding the prose.

## Main repository front door

Keep the existing technical content but reorder the first screen and reduce
duplication between the root README and capability documentation.

Recommended order:

1. Hero, project name, product line, and incubation status.
2. Three concise capability statements:
   - control model spend and keep provider credentials away from agents;
   - constrain and audit tool calls;
   - require bounded human approval for consequential actions.
3. Architecture diagram.
4. Working quickstart: `make up`, then `make chat`.
5. Honest status and limitations.
6. Links into capability-oriented documentation.
7. Detailed command and implementation reference farther down or in `docs/`.

The unbuilt `npx kaimahi create` proposal must not visually outrank the working
`make up && make chat` path. It may remain as a clearly labeled direction after
the visitor has seen what runs today.

### Repository-local community files

Only add local files when the main repository needs details beyond the
organization defaults:

- `CONTRIBUTING.md`: local setup, tests, PR expectations, and links to
  `docs/COORDINATION.md` where relevant.
- `SECURITY.md`: repository-specific supported-version and private-reporting
  details if those differ from the organization default.

Avoid copying the organization defaults unchanged into the main repository.

## Repository and organization settings

Apply settings manually or through GitHub tooling where authorized:

- Pin `kaimahi` on the organization profile.
- Set a concise repository description consistent with the product line.
- Add topics: `ai-agents`, `kubernetes`, `kagent`, `mcp`, `governance`,
  `llm-security`, and `agentic-ai`.
- Upload `brand/social-preview.png` as the repository social preview.
- Prefer squash merging and automatically delete merged branches.
- Protect `main` with required CI, required review for non-maintainer changes,
  stale-approval dismissal, and conversation resolution.
- Keep force pushes and branch deletion disabled on `main`.
- Enable private vulnerability reporting, dependency graph, Dependabot alerts,
  and secret scanning where GitHub makes them available.
- Add CodeQL only if its signal and runtime cost are acceptable for the Go and
  JavaScript present; do not add security theater that nobody will triage.

## Community files

### `CONTRIBUTING.md`

Explain how to find work, reproduce the local environment, run the relevant
checks, make verification claims, and open a focused PR. Preserve the project's
existing “run it before documenting it as working” standard.

### `SECURITY.md`

Provide a private reporting path, what information a useful report should
contain, expected acknowledgment language without an unrealistic SLA, and a
warning not to file exploitable details publicly.

### `SUPPORT.md`

Route usage questions and reproducible bugs to the appropriate place. Do not
promise production support for an incubation project.

### Deferred code of conduct

Do not add `CODE_OF_CONDUCT.md` until the project has a private, monitored
conduct-reporting channel. GitHub private vulnerability reporting is reserved
for security reports and must not be repurposed for conduct complaints.

### Issue forms

- Bug: version/commit, environment, cluster type, reproduction, expected and
  actual result, relevant sanitized logs, and explicit confirmation that no
  secrets are included.
- Feature: user problem, desired outcome, alternatives, and effect on the
  project's governance thesis.
- Config: enable blank issues only if maintainers want exploratory design
  discussions outside structured forms; otherwise route questions explicitly.

### Pull request template

Ask for the problem, approach, verification performed, documentation effect,
security/governance effect, and any claim that is demonstrated once rather than
continuously tested.

## Rollout sequence

1. Produce and approve the mascot concept and compact mark.
2. Produce the hero and social-preview compositions from the approved concept.
3. Create the precise architecture diagram.
4. Add canonical `brand/` sources and usage notes to `kaimahi`.
5. Update `.github/profile/README.md` and add organization community defaults.
6. Rework the main README's opening hierarchy while preserving factual claims.
7. Apply organization/repository metadata and security settings.
8. Verify every link, image crop, inherited community file, issue form, and
   README appearance before merging.

## Acceptance criteria

- A visitor can identify Kaimahi's purpose, incubation status, working
  quickstart, and three core governance capabilities without scrolling through
  the full command reference.
- The organization avatar remains recognizable at 40 px and has no unreadable
  embedded text.
- The README hero and social preview use the same mascot, palette, and product
  line without relying on identical crops.
- The architecture diagram accurately maps controls to the LLM and MCP seams
  and does not make broader enforcement claims than the docs support.
- Organization-level community defaults render and inherit as intended.
- All issue forms validate in GitHub and explicitly discourage secret-bearing
  logs.
- All Markdown links and image references pass the repository's existing link
  checks.
- The main README continues to distinguish built, demonstrated-once,
  schema-valid, proposed, and unbuilt capabilities.
- No `GOVERNANCE.md` is added.
- No `CODE_OF_CONDUCT.md` is added until a private reporting channel is chosen.

## Risks and mitigations

- **Mascot looks childish:** keep proportions compact and illustrative rather
  than chibi; emphasize tools, posture, and purposeful action.
- **Mascot obscures the product:** always pair it with the direct product line
  and a precise architecture diagram.
- **Cultural overreach:** do not borrow Māori visual language without relevant
  collaboration; describe the name factually and keep the mascot rooted in the
  independent overnight-helper story.
- **README claims drift:** keep detailed capability status in the existing docs
  and link to it; automate relative-link checks already present in the repo.
- **Premature repository sprawl:** retain the monorepo until independently
  versioned artifacts and maintainers create an operational reason to split.
