#!/usr/bin/env bash
# Print the kaimahi-proxy pods that can take traffic right now, one per
# line: Ready and NOT terminating. `status.phase=Running` alone is not
# that — a pod draining after a rolling restart stays Running (and keeps
# its IP) until its grace period ends, and a port-forward to it fails.
# Every probe that needs "the replicas" uses this.
#
# Usage: KUBECTL="kubectl --context ..." plane-pods.sh
set -euo pipefail
KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=kaimahi
# shellcheck disable=SC2086 # KUBECTL deliberately carries --context args
$KUBECTL -n "$NAMESPACE" get pods -l app=kaimahi-proxy -o json | python3 -c '
import json, sys
for p in json.load(sys.stdin)["items"]:
    if p["metadata"].get("deletionTimestamp"):
        continue
    conds = p.get("status", {}).get("conditions", [])
    if any(c["type"] == "Ready" and c["status"] == "True" for c in conds):
        print(p["metadata"]["name"])
'
