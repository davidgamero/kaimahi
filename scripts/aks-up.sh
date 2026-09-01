#!/usr/bin/env bash
# Provision the Azure side of the P5b managed-cluster path: a resource
# group, a PRIVATE Azure Container Registry, and an AKS cluster with pull
# rights on that registry.
#
# Everything is parameterised. No subscription, tenant, resource-group,
# registry or cluster identifier belongs in this repo — the repo is
# public, and a committed name is a standing invitation to squat it or to
# fingerprint the owner. You supply them:
#
#   AKS_RESOURCE_GROUP   required   resource group to CREATE (must not exist)
#   ACR_NAME             required   globally-unique registry name (5-50 alnum)
#   AKS_CLUSTER          optional   cluster + kube-context name (default kaimahi)
#   AKS_LOCATION         optional   default westus3
#   AKS_NODE_SIZE        optional   default Standard_B4ms
#   AKS_NODE_COUNT       optional   default 1
#
# Cost shape (see docs/P5B-RUNBOOK.md): control plane on the Free tier is
# $0; the node and a Standard load balancer are the running cost; ACR
# Basic is a small daily charge. This is an EPHEMERAL cluster — create it,
# prove the thing, run scripts/aks-down.sh.
#
# Safety: this script CREATES a resource group and refuses to adopt one it
# did not create. Everything it makes is tagged, and scripts/aks-down.sh
# deletes only what carries that tag — so a mistyped group name can never
# turn teardown into someone else's outage.
set -euo pipefail
umask 077

RG="${AKS_RESOURCE_GROUP:-}"
ACR="${ACR_NAME:-}"
CLUSTER="${AKS_CLUSTER:-kaimahi}"
LOCATION="${AKS_LOCATION:-westus3}"
NODE_SIZE="${AKS_NODE_SIZE:-Standard_B4ms}"
NODE_COUNT="${AKS_NODE_COUNT:-1}"

# The tag that makes teardown safe. Must match scripts/aks-down.sh.
OWNER_TAG_KEY=kaimahi-ephemeral
OWNER_TAG_VALUE=p5b

usage() {
  echo "usage: AKS_RESOURCE_GROUP=<new-rg> ACR_NAME=<unique-name> $0" >&2
  echo "  optional: AKS_CLUSTER AKS_LOCATION AKS_NODE_SIZE AKS_NODE_COUNT" >&2
}

[ -n "$RG" ] || { echo "aks-up: AKS_RESOURCE_GROUP is required" >&2; usage; exit 1; }
[ -n "$ACR" ] || { echo "aks-up: ACR_NAME is required" >&2; usage; exit 1; }

# ACR names are globally unique, alphanumeric only, 5-50 chars. Check
# locally so a bad name fails in a second rather than after the group and
# cluster already exist.
case "$ACR" in
  *[!a-zA-Z0-9]*) echo "aks-up: ACR_NAME must be alphanumeric only" >&2; exit 1 ;;
esac
if [ "${#ACR}" -lt 5 ] || [ "${#ACR}" -gt 50 ]; then
  echo "aks-up: ACR_NAME must be 5-50 characters" >&2
  exit 1
fi

command -v az >/dev/null 2>&1 || { echo "aks-up: the az CLI is not installed" >&2; exit 1; }
az account show >/dev/null 2>&1 || {
  echo "aks-up: not logged in — run: az login" >&2; exit 1; }

# --- resource group -------------------------------------------------------
# Refuse to touch a pre-existing group. Adopting one would mean teardown
# later deletes resources this script never created.
if az group exists --name "$RG" | grep -qx true; then
  existing=$(az group show --name "$RG" \
    --query "tags.\"$OWNER_TAG_KEY\"" -o tsv 2>/dev/null || true)
  if [ "$existing" != "$OWNER_TAG_VALUE" ]; then
    echo "aks-up: resource group '$RG' already exists and is not tagged" >&2
    echo "  $OWNER_TAG_KEY=$OWNER_TAG_VALUE. Refusing to build inside a group" >&2
    echo "  this script did not create — pick a fresh AKS_RESOURCE_GROUP." >&2
    exit 1
  fi
  echo "aks-up: reusing the ephemeral group '$RG' (tagged, created by this script)" >&2
else
  echo "aks-up: creating resource group '$RG' in $LOCATION" >&2
  az group create --name "$RG" --location "$LOCATION" \
    --tags "$OWNER_TAG_KEY=$OWNER_TAG_VALUE" \
    --output none
fi

# --- registry -------------------------------------------------------------
# PRIVATE by design (D15): --admin-enabled is left off, so the only way in
# is Entra auth. Publishing a public image would be an outward-facing
# artifact and a soft claim on a provisional project name.
if az acr show --name "$ACR" --resource-group "$RG" >/dev/null 2>&1; then
  echo "aks-up: registry '$ACR' already present" >&2
else
  echo "aks-up: creating private ACR '$ACR' (Basic, admin user disabled)" >&2
  az acr create --name "$ACR" --resource-group "$RG" --location "$LOCATION" \
    --sku Basic --output none
fi

# --- cluster --------------------------------------------------------------
if az aks show --name "$CLUSTER" --resource-group "$RG" >/dev/null 2>&1; then
  echo "aks-up: cluster '$CLUSTER' already present" >&2
else
  echo "aks-up: creating AKS '$CLUSTER' ($NODE_COUNT x $NODE_SIZE, Free tier)" >&2
  echo "  this takes a few minutes..." >&2
  # --tier free: no control-plane charge and no SLA, which is right for an
  #   ephemeral demo cluster.
  # --attach-acr: grants the kubelet identity AcrPull on the registry, so
  #   the proxy image is pulled with no imagePullSecret anywhere.
  # --node-osdisk-size 32: the smallest managed OS disk that fits; nothing
  #   here is stored on the node.
  az aks create \
    --name "$CLUSTER" --resource-group "$RG" --location "$LOCATION" \
    --node-count "$NODE_COUNT" --node-vm-size "$NODE_SIZE" \
    --node-osdisk-size 32 \
    --tier free \
    --generate-ssh-keys \
    --attach-acr "$ACR" \
    --output none
fi

# Whether the cluster was just created or already existed, the kubelet
# identity must actually hold AcrPull on the registry — without it the
# proxy pod fails with ImagePullBackOff and the cause is two layers away
# from the symptom.
#
# VERIFY, then repair. `az aks update --attach-acr` is idempotent but runs
# a full cluster update (~2 min) even when nothing needs doing, so a blind
# re-run taxes every invocation. Reading the role assignment is seconds,
# and it checks the thing that actually matters rather than assuming a
# command that returned 0 produced the effect (standing guidance: accept
# only a well-formed positive).
echo "aks-up: checking AcrPull for the cluster's kubelet identity" >&2
acr_id=$(az acr show --name "$ACR" --resource-group "$RG" --query id -o tsv)
kubelet_oid=$(az aks show --name "$CLUSTER" --resource-group "$RG" \
  --query identityProfile.kubeletidentity.objectId -o tsv)
if [ -z "$acr_id" ] || [ -z "$kubelet_oid" ]; then
  echo "aks-up: could not resolve the registry or kubelet identity — refusing to guess" >&2
  exit 1
fi

has_acrpull() {
  az role assignment list --assignee "$kubelet_oid" --scope "$acr_id" \
    --query "[?roleDefinitionName=='AcrPull'] | length(@)" -o tsv 2>/dev/null
}

if [ "$(has_acrpull)" = 0 ]; then
  echo "aks-up: AcrPull missing — attaching the registry (a few minutes)" >&2
  az aks update --name "$CLUSTER" --resource-group "$RG" \
    --attach-acr "$ACR" --output none
fi

# Fail closed on the claim itself, and give role propagation a moment:
# the assignment is created asynchronously, so a create that raced would
# otherwise surface much later as an unexplained ImagePullBackOff.
for _ in $(seq 1 12); do
  [ "$(has_acrpull)" != 0 ] && break
  sleep 5
done
if [ "$(has_acrpull)" = 0 ]; then
  echo "aks-up: the kubelet identity still has no AcrPull on '$ACR'." >&2
  echo "  The proxy image would fail to pull. Check whether your account can" >&2
  echo "  create role assignments, then re-run." >&2
  exit 1
fi
echo "aks-up: AcrPull confirmed" >&2

# --- kubeconfig -----------------------------------------------------------
# The context is named after the cluster. Note this is NOT a kind context,
# so every mutating make target will demand confirmation (scripts/kube-guard.sh).
echo "aks-up: writing kubeconfig context '$CLUSTER'" >&2
az aks get-credentials --name "$CLUSTER" --resource-group "$RG" \
  --overwrite-existing --output none

kubectl --context "$CLUSTER" get nodes

cat >&2 <<EOF

aks-up: ready.

  context:   $CLUSTER   (NOT a kind context — targets will ask to confirm)
  registry:  $ACR.azurecr.io
  teardown:  AKS_RESOURCE_GROUP=$RG KAIMAHI_CONFIRM=$RG make aks-down
             ^ do not skip this. The confirmation names the RESOURCE
               GROUP, not the cluster — see docs/P5B-RUNBOOK.md step 7.

Next (see docs/P5B-RUNBOOK.md):
  export TARGET=aks AKS_CLUSTER=$CLUSTER ACR_NAME=$ACR
  export KAIMAHI_CONFIRM=$CLUSTER
  make kagent plane plane-copilot-secret govern agent
EOF
