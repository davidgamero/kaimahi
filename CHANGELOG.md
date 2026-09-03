# Changelog

Kaimahi is **pre-1.0 and incubating**. Versions are semantic with the pre-1.0
reading of that word:

- **Patch** (`v0.1.0` → `v0.1.1`) — fixes only. No schema change, no flag
  removed, no behaviour an operator was relying on.
- **Minor** (`v0.1.0` → `v0.2.0`) — anything else, including a breaking one.
  Below 1.0 the minor number is where breaking changes live, so every entry
  that breaks something says so under **Breaking** and says what to do about
  it.
- **Pre-release** (`v0.2.0-rc.1`) — a candidate for the version it names. Go's
  `@latest` ignores these, so a release candidate cannot become anybody's
  default install by accident.

There is no 1.0 promise yet and no support window. What there *is*: the
release job refuses to publish a tag that has no section here, so a version
without notes cannot exist.

Each entry names what changed, and — where it matters — what an operator has
to do. Sections: **Added**, **Changed**, **Fixed**, **Breaking**, **Upgrading**.

## Unreleased

_Nothing yet._

## v0.1.0 — 2026-09-03

The first tagged release: everything the project has built since P1, at a
version you can name. Most of what is below is release plumbing — the product
can now be installed and upgraded without a commit hash — plus the last
capability to land before the tag was cut.

### Added

- **An agent's calls record who it acted for, and credentials expire.** A run
  opened at the inbound door (where a Slack signature has already proved who
  typed the message) is what joins a governed call to a person, so the ledger
  and the tool audit carry an actor the plane itself observed — never a claim
  made by the thing being governed. Every credential issued from now on has a
  deadline (default 30 days, no way to ask for "never"), enforced at the LLM
  proxy, the MCP gateway and the inbound door, failing closed and audited.
  `make credentials` shows what is expiring; `kmx credential renew` moves the
  date without touching the token, so custody is unchanged
  ([docs/identity.md](docs/identity.md)).
- **Tagged releases, built by CI from the tag.** `kmx` binaries for
  linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64, with a
  `checksums.txt` published beside them. The job refuses to publish a build
  that does not report its own tag, and refuses a tag with no entry in this
  file.
- **`go install …/cmd/kmx@latest`** as the install line — no sha, no
  package-manager namespace claimed, and no new tooling to trust: the module
  proxy and the Go checksum database already stand behind it.
- **`kmx version` reports the build's own version**, not only the versions it
  installs. A release binary reports its tag; a `go install` reports the
  version you asked for; a checkout build says it is a checkout build.
- **A released binary deploys the plane at its tag.** `kmx plane` used to
  depend entirely on VCS stamping surviving the build; the tag is now the
  first source it reads, and the release refuses to publish unless the
  plane module's matching `plane/vX.Y.Z` tag exists at the same commit.
- **A documented upgrade path** ([docs/releases.md](docs/releases.md)),
  including what happens when a migration fails halfway (the plane does not
  start), and a CI job that upgrades a plane across a real schema gap with
  live data in it and proves the data survives.

### Upgrading

- From an untagged `go install …@<sha>` build: install `@latest` and run
  `kmx version`. There is no state in `kmx` itself to migrate.
- For the **plane**, see [docs/releases.md](docs/releases.md#upgrading-the-plane).
  Migrations are additive and run at startup under a lock. Two behaviours are
  worth knowing before you upgrade, and both follow the same rule — an
  upgrade never silently widens or voids what an operator already had:
  - past `00008`: tool grants minted before argument binding existed keep
    their old verb-level meaning, and no new one of that kind can be created.
  - past `00010`: credentials that already exist keep a NULL expiry and keep
    working. Expiring a running estate at migration time would be an outage,
    not a control. `kaimahi_credentials_without_expiry` is the gauge for
    shrinking that set; re-issue or renew at your own pace.

### Not in this release

- **No container image is published.** `kmx plane` still builds the plane's
  image locally from the Go module proxy at kmx's own revision, so Go remains
  a prerequisite for the governed half even if you installed a binary. The
  reasoning is in [docs/releases.md](docs/releases.md#why-no-published-image-yet).
- **No package-manager namespace is claimed** — no Homebrew tap, no npm, no
  crates, no PyPI. The name is provisional and no trademark opinion has been
  obtained; claiming namespaces would raise the cost of a rename that may
  still happen. See [docs/NAMING.md](docs/NAMING.md).
