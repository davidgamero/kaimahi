#!/usr/bin/env bash
# Bootstrap the plane's own secrets, idempotently: the Postgres password
# (kaimahi-pg) and the admin API bearer token (kaimahi-admin), both in
# the kaimahi namespace. Values are generated here and travel only
# through pipes and 0600 files — never argv, env listings, YAML, or logs
# (docs/COORDINATION.md security guidance). Existing Secrets are kept:
# regenerating the pg password under a live database would lock the
# proxy out.
set -euo pipefail
umask 077

KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=kaimahi

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

ensure_secret() { # name key
  local name=$1 key=$2
  if $KUBECTL -n "$NAMESPACE" get secret "$name" >/dev/null 2>&1; then
    echo "Secret $name exists; keeping it." >&2
    return
  fi
  # 32 random bytes, hex — generated straight into a 0600 file.
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n' > "$workdir/$key"
  test -s "$workdir/$key" || { echo "entropy read failed for $name" >&2; exit 1; }
  $KUBECTL -n "$NAMESPACE" create secret generic "$name" \
    --from-file="$key=$workdir/$key"
  echo "Secret $name created." >&2
}

$KUBECTL get namespace "$NAMESPACE" >/dev/null 2>&1 || \
  $KUBECTL create namespace "$NAMESPACE"

ensure_secret kaimahi-pg password
ensure_secret kaimahi-admin token
