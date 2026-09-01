#!/usr/bin/env bash
# CLI glue for the Kaimahi proxy's admin plane (issue governed
# credentials, set budgets, read the ledger). The admin port is not on
# any Service: this script port-forwards to the pod, so cluster
# credentials gate every operation before the admin bearer token does.
#
# Custody rules (docs/COORDINATION.md security guidance):
#   - The admin token and any issued governed token travel only through
#     pipes and 0600 files (curl reads the auth header from a file;
#     kubectl reads the secret value with --from-file, and the issued
#     token's dry-run manifest exists only inside the apply pipe) —
#     never argv, env listings, on-disk YAML, or logs.
#   - Fail closed: every step checks a well-formed positive; no -L on
#     any authenticated call.
#
# Usage:
#   plane-admin.sh issue <name>            mint a governed credential and
#                                          store it as the agent-side
#                                          Secret $GOVERNED_SECRET
#                                          (default kaimahi-governed-token;
#                                          the P4b tools credential uses
#                                          GOVERNED_SECRET=kaimahi-tools-token)
#   plane-admin.sh budget <name> <cents|-> <tokens|->   set caps (- = none)
#   plane-admin.sh ledger [name]           show ledger (+ month totals)
#   plane-admin.sh tool-allow <name> <tool,tool|->      replace tool allowlist
#                                          (- = empty: nothing callable)
#   plane-admin.sh tool-allowlist <name>   show the tool allowlist
#   plane-admin.sh tool-audit [name]       show the tool-call audit trail
set -euo pipefail
umask 077

KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=kaimahi
AGENT_NAMESPACE=kagent
GOVERNED_SECRET="${GOVERNED_SECRET:-kaimahi-governed-token}"
ADMIN_PORT="${ADMIN_PORT:-19091}"

workdir=$(mktemp -d)
pf_pid=""
cleanup() {
  [ -n "$pf_pid" ] && kill "$pf_pid" 2>/dev/null || true
  rm -rf "$workdir"
}
trap cleanup EXIT

# Admin auth header into a 0600 file; curl reads it with -H @file so the
# token never appears in argv or the environment.
$KUBECTL -n "$NAMESPACE" get secret kaimahi-admin -o jsonpath='{.data.token}' \
  | base64 -d > "$workdir/token"
test -s "$workdir/token" || { echo "kaimahi-admin secret missing/empty (run make plane)" >&2; exit 1; }
{ printf 'Authorization: Bearer '; cat "$workdir/token"; printf '\n'; } > "$workdir/auth-header"

# --address pins IPv4 explicitly: if the port is already taken, kubectl
# must fail (and this script with it) rather than bind only the v6 side
# while curl talks to whatever squats on 127.0.0.1.
$KUBECTL -n "$NAMESPACE" port-forward --address 127.0.0.1 \
  deploy/kaimahi-proxy "$ADMIN_PORT:9091" >/dev/null 2>&1 &
pf_pid=$!
for _ in $(seq 1 50); do
  curl -fsS -o /dev/null "http://127.0.0.1:$ADMIN_PORT/healthz" 2>/dev/null && break
  sleep 0.2
done
curl -fsS -o /dev/null "http://127.0.0.1:$ADMIN_PORT/healthz" \
  || { echo "admin port-forward failed" >&2; exit 1; }

admin_curl() { # method path [json-body-file] -> body on stdout, status in $status
  local method=$1 path=$2 body="${3:-}"
  local args=(-sS -X "$method" -H @"$workdir/auth-header" \
    -o "$workdir/resp" -w '%{http_code}' "http://127.0.0.1:$ADMIN_PORT$path")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data @"$body")
  status=$(curl "${args[@]}")
}

json_get() { # file key -> stdout (empty if missing)
  python3 -c 'import json,sys
d = json.load(open(sys.argv[1]))
v = d.get(sys.argv[2], "")
sys.stdout.write(v if isinstance(v, str) else "")' "$1" "$2"
}

# Credential names and caps are interpolated into JSON/query strings;
# validate their shape here (the server validates again).
check_name() {
  case "$1" in
    (*[!a-z0-9-]*|'') echo "invalid credential name '$1' (want [a-z0-9-]+)" >&2; exit 2 ;;
  esac
}
check_cap() {
  case "$1" in
    (null) ;;
    (*[!0-9]*|'') echo "invalid cap '$1' (want a non-negative integer or -)" >&2; exit 2 ;;
  esac
}

cmd="${1:-}"
case "$cmd" in
  issue)
    name="${2:?usage: plane-admin.sh issue <name>}"
    check_name "$name"
    printf '{"name": "%s"}\n' "$name" > "$workdir/req"
    admin_curl POST /admin/credentials "$workdir/req"
    if [ "$status" = 409 ]; then
      bound=$($KUBECTL -n "$AGENT_NAMESPACE" get secret "$GOVERNED_SECRET" \
        -o jsonpath='{.metadata.annotations.kaimahi\.dev/credential}' 2>/dev/null || true)
      if [ "$bound" = "$name" ]; then
        echo "Credential '$name' already issued and $GOVERNED_SECRET is bound to it; keeping both." >&2
        exit 0
      fi
      if [ -n "$bound" ]; then
        echo "Secret $GOVERNED_SECRET holds the token for credential '$bound', not '$name' — refusing." >&2
        exit 1
      fi
      echo "Credential '$name' exists in the plane but Secret $GOVERNED_SECRET is missing (or unlabeled)." >&2
      echo "The token is shown exactly once at issue time and cannot be recovered;" >&2
      echo "delete the row (kubectl -n $NAMESPACE exec deploy/kaimahi-postgres -- \\" >&2
      echo "  psql -U kaimahi -c \"DELETE FROM credential WHERE name='$name'\") and re-run." >&2
      exit 1
    fi
    [ "$status" = 201 ] || { echo "issue failed (HTTP $status):" >&2; cat "$workdir/resp" >&2; exit 1; }
    json_get "$workdir/resp" token > "$workdir/governed-token"
    test -s "$workdir/governed-token" || { echo "no token in response" >&2; exit 1; }
    $KUBECTL -n "$AGENT_NAMESPACE" create secret generic "$GOVERNED_SECRET" \
      --from-file=api-key="$workdir/governed-token" \
      --dry-run=client -o yaml | $KUBECTL -n "$AGENT_NAMESPACE" apply -f -
    # Bind the Secret to its credential so a later issue of a DIFFERENT
    # name can detect the mismatch instead of silently reusing this token.
    $KUBECTL -n "$AGENT_NAMESPACE" annotate --overwrite secret "$GOVERNED_SECRET" \
      "kaimahi.dev/credential=$name" >/dev/null
    echo "Governed credential '$name' issued; agent-side Secret $GOVERNED_SECRET created." >&2
    echo "The plane stores only its hash — the real upstream keys stay with the proxy." >&2
    ;;
  budget)
    name="${2:?usage: plane-admin.sh budget <name> <cents|-> <tokens|->}"
    cents="${3:?cents cap (or - for none)}"
    tokens="${4:?token cap (or - for none)}"
    check_name "$name"
    [ "$cents" = - ] && cents=null
    [ "$tokens" = - ] && tokens=null
    check_cap "$cents"; check_cap "$tokens"
    printf '{"credential": "%s", "cap_cents": %s, "cap_tokens": %s}\n' \
      "$name" "$cents" "$tokens" > "$workdir/req"
    admin_curl PUT /admin/budgets "$workdir/req"
    [ "$status" = 204 ] || { echo "budget set failed (HTTP $status):" >&2; cat "$workdir/resp" >&2; exit 1; }
    echo "Budget for '$name': cap_cents=$cents cap_tokens=$tokens (monthly, UTC)." >&2
    ;;
  ledger)
    name="${2:-}"
    [ -z "$name" ] || check_name "$name"
    admin_curl GET "/admin/ledger?credential=$name&limit=50"
    [ "$status" = 200 ] || { echo "ledger read failed (HTTP $status):" >&2; cat "$workdir/resp" >&2; exit 1; }
    python3 - "$workdir/resp" <<'EOF'
import json, sys
d = json.load(open(sys.argv[1]))
rows = d.get("entries") or []
fmt = "%-19s %-12s %-9s %-16s %6s %6s %6s %-8s %s"
print(fmt % ("created (UTC)", "credential", "upstream", "model", "in", "out", "cents", "source", "status"))
for e in rows:
    print(fmt % (e["created_at"][:19], e["credential"], e["upstream"], e["model"][:16],
                 e["input_tokens"], e["output_tokens"], e["cost_cents"], e["cost_source"], e["status"]))
if "month_cents" in d:
    print(f'-- month to date: {d["month_cents"]} cents, {d["month_tokens"]} tokens')
EOF
    ;;
  tool-allow)
    name="${2:?usage: plane-admin.sh tool-allow <name> <tool,tool|->}"
    tools="${3:?tool list (comma-separated, or - for empty = nothing callable)}"
    check_name "$name"
    json_tools=""
    if [ "$tools" != - ]; then
      IFS=, read -ra parts <<< "$tools"
      for t in "${parts[@]}"; do
        case "$t" in
          (*[!A-Za-z0-9._-]*|'') echo "invalid tool name '$t' (want [A-Za-z0-9._-]+)" >&2; exit 2 ;;
        esac
        json_tools="$json_tools${json_tools:+, }\"$t\""
      done
    fi
    printf '{"credential": "%s", "tools": [%s]}\n' "$name" "$json_tools" > "$workdir/req"
    admin_curl PUT /admin/tool-allowlist "$workdir/req"
    [ "$status" = 204 ] || { echo "tool-allow failed (HTTP $status):" >&2; cat "$workdir/resp" >&2; exit 1; }
    echo "Tool allowlist for '$name': [${json_tools:-}] (enforced on tools/call, projected on tools/list)." >&2
    echo "kagent re-discovers the projection on its next RemoteMCPServer reconcile; enforcement is immediate." >&2
    ;;
  tool-allowlist)
    name="${2:?usage: plane-admin.sh tool-allowlist <name>}"
    check_name "$name"
    admin_curl GET "/admin/tool-allowlist?credential=$name"
    [ "$status" = 200 ] || { echo "tool-allowlist read failed (HTTP $status):" >&2; cat "$workdir/resp" >&2; exit 1; }
    python3 -c 'import json,sys
d = json.load(open(sys.argv[1]))
print(f'"'"'{d["credential"]}: {", ".join(d["tools"]) or "(empty — nothing callable)"}'"'"')' "$workdir/resp"
    ;;
  tool-audit)
    name="${2:-}"
    [ -z "$name" ] || check_name "$name"
    admin_curl GET "/admin/tool-audit?credential=$name&limit=50"
    [ "$status" = 200 ] || { echo "tool-audit read failed (HTTP $status):" >&2; cat "$workdir/resp" >&2; exit 1; }
    python3 - "$workdir/resp" <<'EOF'
import json, sys
d = json.load(open(sys.argv[1]))
rows = d.get("entries") or []
fmt = "%-19s %-12s %-12s %-12s %-24s %-8s %6s %s"
print(fmt % ("created (UTC)", "credential", "upstream", "method", "tool", "decision", "status", "detail"))
for e in rows:
    print(fmt % (e["created_at"][:19], e["credential"], e["upstream"], e["method"],
                 e["tool"], e["decision"], e["status"], e["detail"]))
EOF
    ;;
  *)
    echo "usage: plane-admin.sh issue|budget|ledger|tool-allow|tool-allowlist|tool-audit ..." >&2
    exit 2
    ;;
esac
