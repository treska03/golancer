#!/usr/bin/env bash
#
# Shared configuration and helpers for the golancer local-Kubernetes scripts.
# This file is meant to be *sourced* by prepare_cluster.sh and redeploy.sh, not
# run directly.
#
# Every setting can be overridden from the environment, e.g.:
#   GOLANCER_NAMESPACE=lb scripts/kubernetes/redeploy.sh

# Resolve the repo root from this file's location so the scripts work no matter
# what the caller's working directory is.
_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${_LIB_DIR}/../.." && pwd)"

# --- tunables (override via env) ------------------------------------------
CLUSTER_NAME="${GOLANCER_CLUSTER:-golancer-local}"
KIND_CONTEXT="kind-${CLUSTER_NAME}"
IMAGE_NAME="${GOLANCER_IMAGE:-golancer}"
SIM_IMAGE_NAME="${IMAGE_NAME}-simulate"
RELEASE="${GOLANCER_RELEASE:-golancer}"
NAMESPACE="${GOLANCER_NAMESPACE:-golancer}"
CHART_DIR="${GOLANCER_CHART_DIR:-${REPO_ROOT}/kubernetes/helm}"
BUILD_SCRIPT="${REPO_ROOT}/docker/build_image.sh"

# --- logging --------------------------------------------------------------
if [[ -t 2 ]]; then
  _C_BLUE=$'\033[34m'; _C_YELLOW=$'\033[33m'; _C_RED=$'\033[31m'
  _C_DIM=$'\033[2m'; _C_OFF=$'\033[0m'
else
  _C_BLUE=''; _C_YELLOW=''; _C_RED=''; _C_DIM=''; _C_OFF=''
fi

log()  { printf '%s==>%s %s\n' "${_C_BLUE}"  "${_C_OFF}" "$*" >&2; }
warn() { printf '%swarn:%s %s\n' "${_C_YELLOW}" "${_C_OFF}" "$*" >&2; }
die()  { printf '%serror:%s %s\n' "${_C_RED}"  "${_C_OFF}" "$*" >&2; exit 1; }

# run echoes a command (dimmed) then executes it, so script output shows exactly
# what ran against the cluster.
run() {
  printf '%s    $ %s%s\n' "${_C_DIM}" "$*" "${_C_OFF}" >&2
  "$@"
}

require_cmd() {
  local missing=()
  local c
  for c in "$@"; do
    command -v "${c}" >/dev/null 2>&1 || missing+=("${c}")
  done
  if (( ${#missing[@]} > 0 )); then
    die "required command(s) not found on PATH: ${missing[*]}"
  fi
}

cluster_exists() {
  kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"
}
