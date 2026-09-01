#!/usr/bin/env bash
# Refuse to carry Azure identifiers in the tree (P5b guardrail).
#
# This repo is public. A committed subscription or tenant GUID
# fingerprints the owner; a committed resource-group name, registry login
# server or cluster FQDN names live infrastructure and invites squatting
# on the registry name. The managed-cluster path is therefore entirely
# parameterised, and this check is what keeps it that way — including
# while the AKS run is being written up, which is exactly when a pasted
# terminal transcript is most likely to carry one.
#
# What is refused:
#   - GUIDs (subscription and tenant ids are the usual leak)
#   - *.azmk8s.io  (an AKS API-server FQDN is per-cluster and per-tenant)
#   - a LITERAL <name>.azurecr.io — registry login servers must always be
#     built from a variable or an obvious placeholder, never a real name
#
# Scope: what is actually IN the repo — tracked files plus untracked ones
# git would accept. "In the tree" is the claim being checked, and a
# developer's working directory is full of gitignored run artifacts
# (chat.out, ledger dumps) whose A2A task UUIDs would turn a precise gate
# into noise people learn to ignore. Pass explicit paths to scan those
# instead.
#
# Run:  bash scripts/check-no-azure-ids.sh [path...]
set -euo pipefail

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

if [ "$#" -gt 0 ]; then
  find "$@" -type f -print0 > "$workdir/files"
elif git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git ls-files -z --cached --others --exclude-standard > "$workdir/files"
else
  find . -type f -print0 > "$workdir/files"
fi

python3 - "$workdir/files" <<'PY'
import re, sys

SKIP_DIRS = {".git", "bin", ".claude", "node_modules"}
SKIP_SUFFIX = {".png", ".jpg", ".gif", ".pdf", ".sha256", ".sum"}

GUID = re.compile(r"\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-"
                  r"[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b")
AKS_FQDN = re.compile(r"[A-Za-z0-9-]+\.[a-z0-9-]+\.azmk8s\.io")
# Anything ending in .azurecr.io. A registry reference is only acceptable
# when the name part is a shell/make variable or a visible placeholder.
# \x27 is the apostrophe: written as an escape so this regex carries no
# single quote of its own, which would otherwise have to be escaped out
# of the surrounding shell quoting and become unreadable.
ACR = re.compile(r"(?P<name>[^\s\"\x27`/=]*)\.azurecr\.io")
PLACEHOLDER = re.compile(r"""(
      \$\(?\{?[A-Za-z_][A-Za-z0-9_]*\}?\)?   # $ACR, ${ACR}, $(ACR_NAME)
    | <[^>]*>                                 # <your-registry>
    | ^$                                      # bare ".azurecr.io"
)$""", re.X)

import pathlib
findings = []
for raw in open(sys.argv[1], "rb").read().split(b"\0"):
    if not raw:
        continue
    p = pathlib.Path(raw.decode("utf-8", "replace"))
    if p.suffix in SKIP_SUFFIX or SKIP_DIRS & set(p.parts):
        continue
    try:
        text = p.read_text()
    except (UnicodeDecodeError, OSError):
        continue
    for n, line in enumerate(text.splitlines(), 1):
        for m in GUID.finditer(line):
            # Obviously-synthetic GUIDs (00000000-...-000000000099 and
            # friends) are test fixtures, not identifiers. A real Azure
            # subscription/tenant id is random, so it never has this
            # little entropy. Keep the exemption tight.
            if len(set(m.group(0).replace("-", ""))) <= 4:
                continue
            findings.append((p, n, "GUID (subscription/tenant id?)", m.group(0)))
        for m in AKS_FQDN.finditer(line):
            findings.append((p, n, "AKS cluster FQDN", m.group(0)))
        for m in ACR.finditer(line):
            if not PLACEHOLDER.match(m.group("name")):
                findings.append((p, n, "literal ACR login server", m.group(0)))

for path, n, why, what in findings:
    print(f"{path}:{n}: {why}: {what}")
if findings:
    print(f"\n{len(findings)} Azure identifier(s) in the tree — this repo is public.")
    print("Parameterise them (env vars / <placeholders>) and redact pasted evidence.")
    sys.exit(1)
print("no Azure identifiers in the tree")
PY
