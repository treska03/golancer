#!/usr/bin/env bash
#
# Build a golancer container image. Two images are available:
#
#   default      docker/Dockerfile           -> golancer
#                The balancer itself, with config.yaml baked in (static
#                backends on 127.0.0.1:2115/2215/2315; more can be registered
#                at runtime via the registry API on :8081).
#
#   --simulate   docker/Dockerfile.simulate  -> golancer-simulate
#                scripts/simulate_server: fake backends on 127.0.0.1. Run it as
#                a sidecar next to golancer (the Helm chart does this).
#
# The image reference (name:tag) is printed on stdout so callers (e.g.
# redeploy.sh) can capture it; all human-readable logs go to stderr.
#
# Usage:
#   docker/build_image.sh [-s|--simulate] [-t TAG] [-n NAME] [--push]
#
# Examples:
#   docker/build_image.sh                     # golancer:latest
#   docker/build_image.sh --simulate -t dev   # golancer-simulate:dev
#   docker/build_image.sh -n myrepo/golancer -t 1.2.3 --push
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${GOLANCER_IMAGE:-golancer}"
IMAGE_TAG="latest"
DOCKERFILE="${SCRIPT_DIR}/Dockerfile"
VARIANT="default"
PUSH="false"

usage() {
  awk 'NR==1 {next} /^#/ {sub(/^# ?/, ""); print; next} {exit}' "${BASH_SOURCE[0]}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -s|--simulate)
      DOCKERFILE="${SCRIPT_DIR}/Dockerfile.simulate"
      VARIANT="simulate"
      IMAGE_NAME="${IMAGE_NAME}-simulate"
      shift
      ;;
    -t|--tag)
      IMAGE_TAG="${2:?--tag requires a value}"
      shift 2
      ;;
    -n|--name)
      IMAGE_NAME="${2:?--name requires a value}"
      shift 2
      ;;
    --push)
      PUSH="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

command -v docker >/dev/null 2>&1 || { echo "error: docker not found on PATH" >&2; exit 1; }
[[ -f "${DOCKERFILE}" ]] || { echo "error: Dockerfile not found: ${DOCKERFILE}" >&2; exit 1; }

IMAGE_REF="${IMAGE_NAME}:${IMAGE_TAG}"

echo "==> Building ${IMAGE_REF} (${VARIANT} variant) from ${DOCKERFILE#"${REPO_ROOT}"/}" >&2
# The build context is the repo root: the Dockerfiles COPY go.mod, the source
# tree and the config file from there.
docker build \
  --file "${DOCKERFILE}" \
  --tag "${IMAGE_REF}" \
  "${REPO_ROOT}" >&2

if [[ "${PUSH}" == "true" ]]; then
  echo "==> Pushing ${IMAGE_REF}" >&2
  docker push "${IMAGE_REF}" >&2
fi

# Machine-readable result on stdout.
echo "${IMAGE_REF}"
