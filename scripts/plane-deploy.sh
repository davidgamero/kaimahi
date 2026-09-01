#!/usr/bin/env bash
# Apply k8s/plane/ to the target cluster (P5b: kind or any registry-backed
# cluster such as AKS).
#
# The only thing that differs between environments is how the proxy pod
# gets its image:
#
#   kind — `make plane-image` side-loads the image with `kind load`, and
#          k8s/plane/proxy.yaml pins `imagePullPolicy: Never`. That pin is
#          deliberate (P4a deviation, P4b deviation 6): a side-loaded
#          LOCAL tag must never silently fall back to PULLING a squattable
#          public name. It stays exactly as committed.
#
#   other — the image comes from a registry (P5b: a PRIVATE ACR), so both
#          the image reference and the pull policy must change. `Never`
#          there means ErrImageNeverPull, forever.
#
# So the kind path runs the SAME command it always ran — literally
# `kubectl apply -f k8s/plane/`, no rendering, no transform — which is
# what makes "kind is unchanged" a fact rather than a claim. Only a
# non-kind target renders proxy.yaml, and only its image/pullPolicy.
#
# Fail closed: the render must produce exactly the intended change, and
# the script verifies that before anything is applied.
#
# Env:
#   KUBECTL             kubectl invocation incl. --context (required in practice)
#   PLANE_TARGET        kind (default) | registry
#   PLANE_IMAGE         image reference to deploy (registry targets)
#   PLANE_PULL_POLICY   imagePullPolicy for registry targets (default IfNotPresent)
set -euo pipefail

KUBECTL="${KUBECTL:-kubectl}"
PLANE_TARGET="${PLANE_TARGET:-kind}"
PLANE_IMAGE="${PLANE_IMAGE:-}"
PLANE_PULL_POLICY="${PLANE_PULL_POLICY:-IfNotPresent}"

here=$(cd "$(dirname "$0")/.." && pwd)
manifests="$here/k8s/plane"

if [ "$PLANE_TARGET" = kind ]; then
  # shellcheck disable=SC2086 # KUBECTL deliberately carries --context args
  exec $KUBECTL apply -f "$manifests/"
fi

if [ -z "$PLANE_IMAGE" ]; then
  echo "plane-deploy: PLANE_IMAGE is required for a $PLANE_TARGET target" >&2
  exit 1
fi
case "$PLANE_PULL_POLICY" in
  Always | IfNotPresent) ;;
  *)
    echo "plane-deploy: refusing pull policy '$PLANE_PULL_POLICY' on a registry target" >&2
    echo "  (Never cannot work off-cluster — the image would never be fetched)" >&2
    exit 1
    ;;
esac

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

# Render proxy.yaml with the registry image and a real pull policy. Done
# on the parsed document, not with sed: the string "imagePullPolicy:
# Never" also appears in this repo's comments, and a textual substitution
# that hit a comment (or missed the field) would be silent.
PLANE_IMAGE="$PLANE_IMAGE" PLANE_PULL_POLICY="$PLANE_PULL_POLICY" \
  python3 - "$manifests/proxy.yaml" > "$workdir/proxy.yaml" <<'PY'
import os, sys, yaml

src = sys.argv[1]
image = os.environ["PLANE_IMAGE"]
policy = os.environ["PLANE_PULL_POLICY"]

docs = list(yaml.safe_load_all(open(src)))
patched = 0
for doc in docs:
    if not doc or doc.get("kind") != "Deployment":
        continue
    for c in doc["spec"]["template"]["spec"]["containers"]:
        if c["name"] != "proxy":
            continue
        c["image"] = image
        c["imagePullPolicy"] = policy
        patched += 1

# Fail closed: if the manifest is ever restructured so the container is no
# longer found, deploying the unrendered document would put `Never` on a
# registry cluster and wedge it. Refuse instead.
if patched != 1:
    sys.exit(f"plane-deploy: expected exactly 1 proxy container to render, found {patched}")

yaml.safe_dump_all(docs, sys.stdout, default_flow_style=False, sort_keys=False)
PY

# Verify the render actually says what we intend before it is applied.
PLANE_IMAGE="$PLANE_IMAGE" PLANE_PULL_POLICY="$PLANE_PULL_POLICY" \
  python3 - "$workdir/proxy.yaml" <<'PY'
import os, sys, yaml

want_image = os.environ["PLANE_IMAGE"]
want_policy = os.environ["PLANE_PULL_POLICY"]
for doc in yaml.safe_load_all(open(sys.argv[1])):
    if not doc or doc.get("kind") != "Deployment":
        continue
    for c in doc["spec"]["template"]["spec"]["containers"]:
        if c["name"] != "proxy":
            continue
        assert c["image"] == want_image, c["image"]
        assert c["imagePullPolicy"] == want_policy, c["imagePullPolicy"]
        # The custody surface must survive the render untouched.
        mounts = {m["mountPath"] for m in c["volumeMounts"]}
        assert "/etc/kaimahi/pg" in mounts and "/etc/kaimahi/admin" in mounts, mounts
PY

echo "plane-deploy: proxy image=$PLANE_IMAGE pullPolicy=$PLANE_PULL_POLICY" >&2

# Everything except proxy.yaml is environment-independent and applied as
# committed; the rendered proxy replaces only that one file.
for f in "$manifests"/*.yaml; do
  [ "$(basename "$f")" = proxy.yaml ] && continue
  # shellcheck disable=SC2086
  $KUBECTL apply -f "$f"
done
# shellcheck disable=SC2086
$KUBECTL apply -f "$workdir/proxy.yaml"
