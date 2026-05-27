#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/lib.sh"
load_env_file "${BOOTSTRAP_ENV_FILE:?}"

require_root
require_cmd go
require_cmd kubectl

REPO_DIR="$(repo_dir)"
RENDER_ROOT="/var/lib/kodex/bootstrap-render"
KODEX_PRODUCTION_NAMESPACE="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
KODEX_ROLLOUT_TIMEOUT="${KODEX_ROLLOUT_TIMEOUT:-300s}"

[ -d "${REPO_DIR}/deploy/base/bootstrap-foundation" ] || die "bootstrap foundation manifests are missing from repository snapshot"

kube_env
install -d -m 700 "${RENDER_ROOT}"
rm -rf "${RENDER_ROOT}/bootstrap-foundation"

log "Render bootstrap foundation manifests"
go run "${REPO_DIR}/cmd/manifest-render" \
  --env-file "${BOOTSTRAP_ENV_FILE}" \
  --source "${REPO_DIR}/deploy/base/bootstrap-foundation" \
  --output "${RENDER_ROOT}/bootstrap-foundation"

log "Apply production namespace and internal registry"
kubectl create namespace "${KODEX_PRODUCTION_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -k "${RENDER_ROOT}/bootstrap-foundation"
kubectl -n "${KODEX_PRODUCTION_NAMESPACE}" rollout status deployment/kodex-registry --timeout="${KODEX_ROLLOUT_TIMEOUT}"

log "Internal registry foundation is ready"
