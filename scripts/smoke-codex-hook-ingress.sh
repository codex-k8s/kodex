#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="smoke-codex-hook-ingress"
RENDER_DIR="${KODEX_SMOKE_RENDER_DIR:-}"
NAMESPACE="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
ROLL_OUT_TIMEOUT="${KODEX_ROLLOUT_TIMEOUT:-300s}"
LOCAL_PORT="${KODEX_CODEX_HOOK_INGRESS_SMOKE_PORT:-18088}"
RESTART_DEPLOYMENT="${KODEX_SMOKE_RESTART_DEPLOYMENT:-true}"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/inventory.sh"

require_commands() {
  local command_name
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "${SCRIPT_NAME}: ${command_name} is required" >&2
      exit 1
    fi
  done
}

cleanup() {
  if [[ -n "${PORT_FORWARD_PID:-}" ]]; then
    kill "$PORT_FORWARD_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${PORT_FORWARD_LOG:-}" ]]; then
    rm -f "$PORT_FORWARD_LOG"
  fi
  if [[ "${RENDER_DIR_IS_TEMP:-false}" == "true" && "${KODEX_SMOKE_KEEP_RENDER_DIR:-false}" != "true" ]]; then
    rm -rf "$RENDER_DIR"
  fi
}

kubectl_args=()
if [[ -n "${KUBECONFIG:-}" ]]; then
  kubectl_args+=(--kubeconfig "$KUBECONFIG")
fi

kodex_kubectl() {
  kubectl "${kubectl_args[@]}" "$@"
}

require_commands go kubectl curl

if [[ -z "$RENDER_DIR" ]]; then
  RENDER_DIR="$(mktemp -d)"
  RENDER_DIR_IS_TEMP="true"
else
  rm -rf "$RENDER_DIR"
  RENDER_DIR_IS_TEMP="false"
fi
trap cleanup EXIT

export KODEX_PRODUCTION_NAMESPACE="$NAMESPACE"
export KODEX_CODEX_HOOK_INGRESS_IMAGE="${KODEX_CODEX_HOOK_INGRESS_IMAGE:-$(kodex_image_from_repo KODEX_CODEX_HOOK_INGRESS_IMAGE KODEX_CODEX_HOOK_INGRESS_INTERNAL_IMAGE_REPOSITORY kodex/codex-hook-ingress KODEX_CODEX_HOOK_INGRESS_VERSION codex-hook-ingress)}"

if [[ -z "$KODEX_CODEX_HOOK_INGRESS_IMAGE" ]]; then
  echo "${SCRIPT_NAME}: KODEX_CODEX_HOOK_INGRESS_IMAGE must be populated before apply" >&2
  exit 1
fi

go run "${PROJECT_ROOT}/cmd/manifest-render" \
  --source "${PROJECT_ROOT}/deploy/base/codex-hook-ingress" \
  --output "${RENDER_DIR}/codex-hook-ingress"

kodex_kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kodex_kubectl apply -f -
kodex_kubectl apply -k "${RENDER_DIR}/codex-hook-ingress"
if [[ "$RESTART_DEPLOYMENT" == "true" ]]; then
  kodex_kubectl -n "$NAMESPACE" rollout restart deployment/codex-hook-ingress
fi
kodex_kubectl -n "$NAMESPACE" rollout status deployment/codex-hook-ingress --timeout="$ROLL_OUT_TIMEOUT"

PORT_FORWARD_LOG="$(mktemp)"
kodex_kubectl -n "$NAMESPACE" port-forward svc/codex-hook-ingress "${LOCAL_PORT}:8080" >"$PORT_FORWARD_LOG" 2>&1 &
PORT_FORWARD_PID="$!"

ready="false"
for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:${LOCAL_PORT}/health/readyz" >/dev/null; then
    ready="true"
    break
  fi
  sleep 1
done
if [[ "$ready" != "true" ]]; then
  cat "$PORT_FORWARD_LOG" >&2 || true
  echo "${SCRIPT_NAME}: readyz did not become healthy" >&2
  exit 1
fi

curl -fsS "http://127.0.0.1:${LOCAL_PORT}/health/livez" >/dev/null
curl -fsS "http://127.0.0.1:${LOCAL_PORT}/health/readyz" >/dev/null
curl -fsS "http://127.0.0.1:${LOCAL_PORT}/metrics" >/dev/null

echo "${SCRIPT_NAME}: health, readiness and metrics OK"
