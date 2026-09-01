#!/usr/bin/env bash
# Tear down everything scripts/aks-up.sh created: the AKS cluster, the
# private ACR, and the resource group holding them — plus the kubeconfig
# entries, so a dead context cannot be targeted later.
#
# This is the most dangerous command in the repo, so it is the most
# suspicious one. `az group delete` is recursive and irreversible, and a
# subscription typically holds many groups that are emphatically not
# ours. Two independent gates therefore stand in front of it:
#
#   1. TAG PROOF. The group must carry the tag scripts/aks-up.sh sets.
#      A group we did not create cannot be deleted by this script at all,
#      no matter what is typed or confirmed. This is the gate that makes a
#      typo'd group name harmless instead of catastrophic.
#   2. EXPLICIT CONFIRMATION naming the group (KAIMAHI_CONFIRM, or an
#      interactive answer). Fail closed: no TTY and no KAIMAHI_CONFIRM
#      means no deletion.
#
#   AKS_RESOURCE_GROUP   required   the group to delete
#   AKS_CLUSTER          optional   kube-context to remove (default kaimahi)
set -euo pipefail

RG="${AKS_RESOURCE_GROUP:-}"
CLUSTER="${AKS_CLUSTER:-kaimahi}"

# Must match scripts/aks-up.sh.
OWNER_TAG_KEY=kaimahi-ephemeral
OWNER_TAG_VALUE=p5b

[ -n "$RG" ] || {
  echo "aks-down: AKS_RESOURCE_GROUP is required" >&2
  echo "usage: AKS_RESOURCE_GROUP=<rg> [AKS_CLUSTER=<name>] $0" >&2
  exit 1; }

command -v az >/dev/null 2>&1 || { echo "aks-down: the az CLI is not installed" >&2; exit 1; }
az account show >/dev/null 2>&1 || {
  echo "aks-down: not logged in — run: az login" >&2; exit 1; }

if ! az group exists --name "$RG" | grep -qx true; then
  echo "aks-down: resource group '$RG' does not exist — nothing to delete." >&2
  # Still clean up any stale kubeconfig entries for the cluster name.
  kubectl config delete-context "$CLUSTER" >/dev/null 2>&1 || true
  kubectl config delete-cluster "$CLUSTER" >/dev/null 2>&1 || true
  kubectl config delete-user "clusterUser_${RG}_${CLUSTER}" >/dev/null 2>&1 || true
  exit 0
fi

# --- gate 1: tag proof ----------------------------------------------------
tag=$(az group show --name "$RG" --query "tags.\"$OWNER_TAG_KEY\"" -o tsv 2>/dev/null || true)
if [ "$tag" != "$OWNER_TAG_VALUE" ]; then
  echo "aks-down: REFUSING to delete resource group '$RG'." >&2
  echo "  It does not carry $OWNER_TAG_KEY=$OWNER_TAG_VALUE, so this script did" >&2
  echo "  not create it. Nothing was deleted." >&2
  exit 1
fi

echo "----------------------------------------------------------------" >&2
echo "  about to DELETE resource group: $RG" >&2
echo "  tagged:   $OWNER_TAG_KEY=$OWNER_TAG_VALUE (created by scripts/aks-up.sh)" >&2
echo "  contains:" >&2
az resource list --resource-group "$RG" --query "[].{type:type,name:name}" -o tsv 2>/dev/null |
  sed 's/^/    /' >&2 || true
echo "  This is irreversible." >&2
echo "----------------------------------------------------------------" >&2

# --- gate 2: explicit confirmation ---------------------------------------
if [ -n "${KAIMAHI_CONFIRM:-}" ]; then
  if [ "$KAIMAHI_CONFIRM" != "$RG" ]; then
    echo "aks-down: KAIMAHI_CONFIRM does not name this resource group — refusing." >&2
    echo "  to proceed:  KAIMAHI_CONFIRM=$RG make aks-down" >&2
    exit 1
  fi
  echo "aks-down: confirmed via KAIMAHI_CONFIRM." >&2
elif [ -t 0 ]; then
  printf 'Type the resource group name to delete it (anything else aborts): ' >&2
  IFS= read -r answer || answer=""
  if [ "$answer" != "$RG" ]; then
    echo "aks-down: not confirmed — nothing was deleted." >&2
    exit 1
  fi
else
  echo "aks-down: no TTY and no KAIMAHI_CONFIRM — refusing to delete unattended." >&2
  echo "  to proceed:  KAIMAHI_CONFIRM=$RG make aks-down" >&2
  exit 1
fi

# Deliberately NOT --no-wait: teardown is only reportable once it is done,
# and "I asked Azure to delete it" is not the same claim as "it is gone".
echo "aks-down: deleting '$RG' (waiting for completion — this takes a few minutes)" >&2
az group delete --name "$RG" --yes --output none

# Fail closed on the claim itself: confirm the group is actually gone
# before saying so.
if az group exists --name "$RG" | grep -qx true; then
  echo "aks-down: resource group '$RG' still exists after delete returned." >&2
  exit 1
fi

# Kubeconfig hygiene: a context pointing at a deleted cluster is a live
# foot-gun for the next `make` invocation.
kubectl config delete-context "$CLUSTER" >/dev/null 2>&1 || true
kubectl config delete-cluster "$CLUSTER" >/dev/null 2>&1 || true
kubectl config delete-user "clusterUser_${RG}_${CLUSTER}" >/dev/null 2>&1 || true

echo "aks-down: resource group '$RG' deleted; kubeconfig entries removed." >&2
