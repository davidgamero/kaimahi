#!/usr/bin/env bash
# Build the demo's fixture ERP, side-load it into the kind cluster, project
# the corpus as a ConfigMap and roll the Deployment (P13, docs/ap-demo.md).
#
# kind only, deliberately: the image is never published (P13 guardrail), so
# there is nothing for a managed cluster to pull. `make erp` on TARGET=aks
# refuses rather than pretending.
#
# The corpus is k8s/erp-fixtures.json — one source of truth. The Go tests
# validate the arithmetic of that same file (internal/erp/fixtures_test.go),
# and the server refuses to start on a corpus that does not add up, so an
# edit that breaks the story fails at boot instead of answering wrongly.
#
# Steps: --step image | fixtures | deploy (default: all three).
set -euo pipefail
umask 077

KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=kaimahi
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"
KIND_CLUSTER="${KIND_CLUSTER:-kaimahi-p1}"
ERP_IMAGE="${ERP_IMAGE:-kaimahi-erp:dev}"
FIXTURES="${FIXTURES:-k8s/erp-fixtures.json}"
STEP="${1:-all}"

case "$CONTAINER_ENGINE" in
  (docker|podman) ;;
  (*) echo "unknown CONTAINER_ENGINE '$CONTAINER_ENGINE' (want docker or podman)" >&2; exit 2 ;;
esac

do_image() {
  echo "erp: building $ERP_IMAGE with $CONTAINER_ENGINE" >&2
  "$CONTAINER_ENGINE" build -f cmd/kaimahi-erp/Dockerfile -t "$ERP_IMAGE" .
  # The podman half is the plane's, carried across with its reason (see
  # internal/kmx/app/plane.go): `kind load docker-image` reports "image not
  # present locally" for images podman demonstrably has, so non-docker
  # engines go through the archive path kind documents.
  if [ "$CONTAINER_ENGINE" = podman ]; then
    tar=$(mktemp -t kaimahi-erp-XXXXXX.tar)
    trap 'rm -f "$tar"' RETURN
    podman save -o "$tar" "$ERP_IMAGE"
    kind load image-archive "$tar" --name "$KIND_CLUSTER"
  else
    kind load docker-image "$ERP_IMAGE" --name "$KIND_CLUSTER"
  fi
}

do_fixtures() {
  test -s "$FIXTURES" || { echo "no fixture corpus at $FIXTURES" >&2; exit 1; }
  python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$FIXTURES" \
    || { echo "$FIXTURES is not valid JSON" >&2; exit 1; }
  echo "erp: projecting $FIXTURES as ConfigMap kaimahi-erp-fixtures" >&2
  $KUBECTL -n "$NAMESPACE" create configmap kaimahi-erp-fixtures \
    --from-file=fixtures.json="$FIXTURES" \
    --dry-run=client -o yaml | $KUBECTL -n "$NAMESPACE" apply -f -
}

do_deploy() {
  $KUBECTL apply -f k8s/erp-mcp.yaml
  # Always restart: a rebuilt image under the same tag, or an edited
  # corpus, leaves the spec unchanged — apply alone would keep the old
  # binary and the old story running. Same reason `make plane` restarts.
  $KUBECTL -n "$NAMESPACE" rollout restart deploy/kaimahi-erp
  $KUBECTL -n "$NAMESPACE" rollout status deploy/kaimahi-erp --timeout=300s
  # Fail closed on the thing that actually matters: the pod is ready only
  # once the corpus loaded, because the server exits on a corpus that does
  # not add up. Say so, with the numbers, so a bad edit is obvious here.
  $KUBECTL -n "$NAMESPACE" logs -l app=kaimahi-erp --tail=20 | grep 'corpus loaded' \
    || { echo "the ERP did not report a loaded corpus — check its logs" >&2; exit 1; }
}

case "$STEP" in
  (all)      do_image; do_fixtures; do_deploy ;;
  (image)    do_image ;;
  (fixtures) do_fixtures; do_deploy ;;
  (deploy)   do_deploy ;;
  (*) echo "usage: erp-deploy.sh [all|image|fixtures|deploy]" >&2; exit 2 ;;
esac
