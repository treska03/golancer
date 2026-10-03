#!/usr/bin/env bash
#
# Rebuild golancer and roll it out to the local kind cluster in one step:
#
#   1. build the golancer image and the simulated-backends sidecar image
#      (docker/build_image.sh, docker/build_image.sh --simulate)
#   2. load them into the kind cluster (no registry needed)
#   3. helm upgrade --install (installs on first run, upgrades afterwards)
#
# Step 3 always runs, so it also picks up chart/template/values.yaml changes —
# not just new application code. Each run gets a unique image tag by default so
# the pod template always changes and Kubernetes performs a fresh rollout.
#
# Usage:
#   scripts/kubernetes/redeploy.sh                 # build + load + upgrade
#   scripts/kubernetes/redeploy.sh --wait          # also block on rollout
#   scripts/kubernetes/redeploy.sh --no-build -t X # reuse existing image tags
#   scripts/kubernetes/redeploy.sh --no-simulate   # golancer only, no sidecar
#
# Flags:
#   -t, --tag TAG      Image tag to build/deploy for both images
#                      (default: dev-<sha>[-dirty]-<ts>).
#       --no-simulate  Skip the simulated-backends sidecar (simulate.enabled=false).
#       --no-build     Skip docker build; the tag(s) must already exist locally.
#       --wait         Wait for the rollout to become Ready (see the note below).
#   -h, --help         Show this help.
#
# Environment overrides: GOLANCER_CLUSTER, GOLANCER_IMAGE, GOLANCER_RELEASE,
# GOLANCER_NAMESPACE, GOLANCER_CHART_DIR.
#
# NOTE: golancer reports NotReady (/readyz -> 503) until at least one backend is
# healthy. With the sidecar that happens after the first health probe; with
# --no-simulate it stays NotReady until reachable backends are configured, so
# --wait will time out. See scripts/kubernetes/README.md.
#
set -euo pipefail
# shellcheck source=scripts/kubernetes/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

IMAGE_TAG=""
SIMULATE="true"
DO_BUILD="true"
WAIT="false"
HELM_TIMEOUT="${GOLANCER_HELM_TIMEOUT:-120s}"

usage() { awk 'NR==1 {next} /^#/ {sub(/^# ?/, ""); print; next} {exit}' "${BASH_SOURCE[0]}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--tag)   IMAGE_TAG="${2:?--tag requires a value}"; shift 2 ;;
    --no-simulate) SIMULATE="false"; shift ;;
    --no-build)    DO_BUILD="false"; shift ;;
    --wait)        WAIT="true"; shift ;;
    -h|--help)     usage; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
done

require_cmd docker kind helm kubectl
cluster_exists || die "kind cluster '${CLUSTER_NAME}' not found. Run scripts/kubernetes/prepare_cluster.sh first."

# Point kubectl/helm at the kind cluster so we never deploy to the wrong context.
run kubectl config use-context "${KIND_CONTEXT}" >/dev/null

# A unique tag per run guarantees the Deployment's pod template changes, which is
# what triggers a rollout. (With a fixed tag + IfNotPresent, Kubernetes would
# not restart pods even after the image is reloaded.)
if [[ -z "${IMAGE_TAG}" ]]; then
  sha="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo nogit)"
  dirty=""
  git -C "${REPO_ROOT}" diff --quiet 2>/dev/null || dirty="-dirty"
  IMAGE_TAG="dev-${sha}${dirty}-$(date +%s)"
fi
IMAGE_REF="${IMAGE_NAME}:${IMAGE_TAG}"
SIM_IMAGE_REF="${SIM_IMAGE_NAME}:${IMAGE_TAG}"

IMAGE_REFS=("${IMAGE_REF}")
[[ "${SIMULATE}" == "true" ]] && IMAGE_REFS+=("${SIM_IMAGE_REF}")

# 1. Build (or verify existing images when --no-build).
if [[ "${DO_BUILD}" == "true" ]]; then
  log "Building image ${IMAGE_REF}..."
  run "${BUILD_SCRIPT}" --name "${IMAGE_NAME}" --tag "${IMAGE_TAG}" >/dev/null
  if [[ "${SIMULATE}" == "true" ]]; then
    log "Building image ${SIM_IMAGE_REF}..."
    run "${BUILD_SCRIPT}" --simulate --name "${SIM_IMAGE_NAME}" --tag "${IMAGE_TAG}" >/dev/null
  fi
else
  log "Skipping build (--no-build); expecting ${IMAGE_REFS[*]} to already exist."
  for ref in "${IMAGE_REFS[@]}"; do
    docker image inspect "${ref}" >/dev/null 2>&1 \
      || die "image ${ref} not found locally; drop --no-build or pass --tag."
  done
fi

# 2. Load the images into the kind nodes so pods can use them without a registry.
for ref in "${IMAGE_REFS[@]}"; do
  log "Loading ${ref} into kind cluster '${CLUSTER_NAME}'..."
  run kind load docker-image "${ref}" --name "${CLUSTER_NAME}"
done

# 3. Install/upgrade the Helm release with the new image(s).
log "Deploying release '${RELEASE}' into namespace '${NAMESPACE}'..."
helm_args=(
  upgrade --install "${RELEASE}" "${CHART_DIR}"
  --namespace "${NAMESPACE}" --create-namespace
  --set "image.repository=${IMAGE_NAME}"
  --set "image.tag=${IMAGE_TAG}"
  --set "image.pullPolicy=IfNotPresent"
  --set "simulate.enabled=${SIMULATE}"
)
if [[ "${SIMULATE}" == "true" ]]; then
  helm_args+=(
    --set "simulate.image.repository=${SIM_IMAGE_NAME}"
    --set "simulate.image.tag=${IMAGE_TAG}"
    --set "simulate.image.pullPolicy=IfNotPresent"
  )
fi
if [[ "${WAIT}" == "true" ]]; then
  helm_args+=(--wait --timeout "${HELM_TIMEOUT}")
fi
run helm "${helm_args[@]}"

log "Deployed ${IMAGE_REF}. Current pods:"
run kubectl --namespace "${NAMESPACE}" get pods \
  -l "app.kubernetes.io/instance=${RELEASE}" -o wide || true

if [[ "${SIMULATE}" == "true" ]]; then
  cat >&2 <<EOF

${_C_YELLOW}Next:${_C_OFF} drive traffic through the balancer and the simulated backends:

  # terminal 1 - forward proxy, registry API, metrics and one backend:
  kubectl -n ${NAMESPACE} port-forward deploy/${RELEASE} 8080:8080 8081:8081 2112:2112 2115:2115

  # terminal 2 - see how requests are spread, then knock a backend out:
  for i in \$(seq 1 30); do curl -s localhost:8080/; echo; done | jq -r .port | sort | uniq -c
  curl -s localhost:2115/toggle-health

See scripts/kubernetes/README.md for the full workflow.
EOF
else
  cat >&2 <<EOF

${_C_YELLOW}Reminder:${_C_OFF} without the simulated sidecar the pod stays NotReady until
a reachable backend is healthy (/readyz returns 503 otherwise). Register one via
the API after: kubectl -n ${NAMESPACE} port-forward deploy/${RELEASE} 8080:8080 8081:8081

  curl -s -X POST localhost:8081/backends -d '{"url":"https://example.com","weight":1}'
EOF
fi
