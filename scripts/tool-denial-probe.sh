#!/usr/bin/env bash
# Prove the MCP gateway denies a NOT-allowlisted tools/call fail-closed:
# port-forward the gateway Service, call the given tool with the governed
# kmh_ token, and require a JSON-RPC error (no result) naming the
# allowlist. Exits nonzero on any other outcome — including the call
# unexpectedly succeeding.
#
# Custody rules (docs/COORDINATION.md security guidance): the governed
# token travels only through pipes and 0600 files (curl reads the auth
# header from a file) — never argv, env listings, or logs.
#
# Usage: tool-denial-probe.sh <tool-name>
set -euo pipefail
umask 077

KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=kaimahi
AGENT_NAMESPACE=kagent
GOVERNED_SECRET="${GOVERNED_SECRET:-kaimahi-tools-token}"
GATEWAY_PORT="${GATEWAY_PORT:-18081}"
UPSTREAM="${UPSTREAM:-kagent-tools}"

tool="${1:?usage: tool-denial-probe.sh <tool-name>}"
case "$tool" in
  (*[!A-Za-z0-9._-]*|'') echo "invalid tool name '$tool'" >&2; exit 2 ;;
esac

# Context safety (P5b). These probes MUTATE governance state — they
# consume grant uses and write audit rows — and they are the one path
# that bypasses the Makefile's `guard` prerequisite, because CI and
# humans run them directly rather than through a target. Left alone they
# inherit whatever `kubectl config current-context` happens to be, and
# `az aks get-credentials` changes that silently: after provisioning an
# AKS cluster, a probe meant for kind quietly aims at the managed one.
# (Observed while verifying this lane.)
#
# `config view --minify` is what resolves the EFFECTIVE context, honouring
# a --context carried in $KUBECTL; `config current-context` ignores that
# flag and would guard a different cluster than the one acted on.
KUBE_CTX="${KUBE_CTX:-$($KUBECTL config view --minify -o jsonpath='{.contexts[0].name}')}"
KUBE_NS="$NAMESPACE, $AGENT_NAMESPACE" KUBE_CTX="$KUBE_CTX" \
  bash "$(dirname "$0")/kube-guard.sh" "$(basename "$0") $tool"

workdir=$(mktemp -d)
pf_pid=""
cleanup() {
  [ -n "$pf_pid" ] && kill "$pf_pid" 2>/dev/null || true
  rm -rf "$workdir"
}
trap cleanup EXIT

$KUBECTL -n "$AGENT_NAMESPACE" get secret "$GOVERNED_SECRET" \
  -o jsonpath='{.data.api-key}' | base64 -d > "$workdir/token"
test -s "$workdir/token" || { echo "$GOVERNED_SECRET missing/empty (run make govern-tools)" >&2; exit 1; }
{ printf 'Authorization: Bearer '; cat "$workdir/token"; printf '\n'; } > "$workdir/auth-header"

# --address pins IPv4 explicitly (see plane-admin.sh for why).
$KUBECTL -n "$NAMESPACE" port-forward --address 127.0.0.1 \
  svc/kaimahi-mcp-gateway "$GATEWAY_PORT:8081" >/dev/null 2>&1 &
pf_pid=$!
for _ in $(seq 1 50); do
  curl -fsS -o /dev/null "http://127.0.0.1:$GATEWAY_PORT/healthz" 2>/dev/null && break
  sleep 0.2
done
curl -fsS -o /dev/null "http://127.0.0.1:$GATEWAY_PORT/healthz" \
  || { echo "gateway port-forward failed" >&2; exit 1; }

printf '{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "%s", "arguments": {}}}\n' \
  "$tool" > "$workdir/req"
status=$(curl -sS -X POST -H @"$workdir/auth-header" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  --data @"$workdir/req" -o "$workdir/resp" -w '%{http_code}' \
  "http://127.0.0.1:$GATEWAY_PORT/upstream/$UPSTREAM/mcp")
[ "$status" = 200 ] || { echo "expected HTTP 200 carrying a JSON-RPC error, got $status:" >&2; cat "$workdir/resp" >&2; exit 1; }
python3 - "$workdir/resp" "$tool" <<'EOF'
import json, sys
d = json.load(open(sys.argv[1]))
err = d.get("error")
assert "result" not in d, f"tools/call for {sys.argv[2]} unexpectedly returned a result"
assert err and "not permitted" in err.get("message", ""), f"unexpected response: {d}"
print(f'denied as expected: {sys.argv[2]} -> JSON-RPC error {err["code"]}: {err["message"]}')
EOF
