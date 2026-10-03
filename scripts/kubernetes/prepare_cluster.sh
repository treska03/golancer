#!/usr/bin/env bash
#
# Create (or recreate/delete) the local kind cluster that the other golancer
# Kubernetes scripts deploy into. Safe to run repeatedly: by default it is a
# no-op when the cluster already exists.
#
# Usage:
#   scripts/kubernetes/prepare_cluster.sh            # create if missing
#   scripts/kubernetes/prepare_cluster.sh --recreate # delete then create
#   scripts/kubernetes/prepare_cluster.sh --delete   # tear the cluster down
#
# Environment overrides: GOLANCER_CLUSTER (default: golancer-local).
#
set -euo pipefail
# shellcheck source=scripts/kubernetes/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ACTION="create"

usage() { awk 'NR==1 {next} /^#/ {sub(/^# ?/, ""); print; next} {exit}' "${BASH_SOURCE[0]}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --recreate) ACTION="recreate"; shift ;;
    --delete)   ACTION="delete";   shift ;;
    -h|--help)  usage; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
done

require_cmd kind kubectl

create_cluster() {
  if cluster_exists; then
    log "kind cluster '${CLUSTER_NAME}' already exists — nothing to create."
  else
    log "Creating kind cluster '${CLUSTER_NAME}'..."
    run kind create cluster --name "${CLUSTER_NAME}"
  fi
  run kubectl config use-context "${KIND_CONTEXT}" >/dev/null
  log "Active kubectl context: ${KIND_CONTEXT}"
  log "Cluster ready. Deploy golancer with: scripts/kubernetes/redeploy.sh"
}

delete_cluster() {
  if cluster_exists; then
    log "Deleting kind cluster '${CLUSTER_NAME}'..."
    run kind delete cluster --name "${CLUSTER_NAME}"
  else
    log "kind cluster '${CLUSTER_NAME}' does not exist — nothing to delete."
  fi
}

case "${ACTION}" in
  create)   create_cluster ;;
  delete)   delete_cluster ;;
  recreate) delete_cluster; create_cluster ;;
esac
