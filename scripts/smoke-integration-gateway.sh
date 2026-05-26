#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-integration-gateway"
kodex_smoke_require_commands go kubectl curl grep

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_PROVIDER_HUB_IMAGE="$(kodex_image_from_repo KODEX_PROVIDER_HUB_IMAGE KODEX_PROVIDER_HUB_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub KODEX_PROVIDER_HUB_VERSION provider-hub)"
KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE KODEX_PROVIDER_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub-migrations KODEX_PROVIDER_HUB_VERSION provider-hub)"
KODEX_INTEGRATION_GATEWAY_IMAGE="$(kodex_image_from_repo KODEX_INTEGRATION_GATEWAY_IMAGE KODEX_INTEGRATION_GATEWAY_INTERNAL_IMAGE_REPOSITORY kodex/integration-gateway KODEX_INTEGRATION_GATEWAY_VERSION integration-gateway)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED="${KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED:-true}"
KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE="${KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE:-env}"
KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF="${KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF:-KODEX_GITHUB_WEBHOOK_SECRET}"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PROVIDER_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN)
fi
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE, KODEX_PROVIDER_HUB_IMAGE, KODEX_INTEGRATION_GATEWAY_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_IMAGE" \
  "$KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE" \
  "$KODEX_PROVIDER_HUB_IMAGE" \
  "$KODEX_INTEGRATION_GATEWAY_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_ACCESS_MANAGER_IMAGE \
  KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PROVIDER_HUB_IMAGE \
  KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE \
  KODEX_INTEGRATION_GATEWAY_IMAGE \
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED \
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE \
  KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations access-manager access-manager-migrations
kodex_smoke_apply_deployment access-manager access-manager/access-manager.yaml
kodex_smoke_apply_migrations provider-hub provider-hub-migrations
kodex_smoke_apply_deployment provider-hub provider-hub/provider-hub.yaml
kodex_smoke_apply_deployment integration-gateway integration-gateway/integration-gateway.yaml
kodex_smoke_check_readyz integration-gateway 18086

curl -fsS "http://127.0.0.1:18086/health/livez" >/dev/null
curl -fsS "http://127.0.0.1:18086/metrics" >/dev/null
curl -fsS "http://127.0.0.1:18086/openapi/integration-gateway.v1.yaml" | grep -q "/v1/provider-webhooks/{provider_slug}"

response_file="$(mktemp)"
trap 'rm -f "$response_file"; kodex_smoke_cleanup' EXIT

status="$(curl -sS -o "$response_file" -w "%{http_code}" \
  -X POST "http://127.0.0.1:18086/v1/provider-webhooks/github" \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Delivery: smoke-delivery-missing-signature" \
  -H "X-GitHub-Event: ping" \
  --data '{"action":"ping","payload":"safe-smoke"}')"
if [[ "$status" != "401" ]]; then
  cat "$response_file" >&2 || true
  echo "smoke-integration-gateway: missing GitHub signature must be rejected with 401" >&2
  exit 1
fi
if ! grep -q '"code":"signature_invalid"' "$response_file"; then
  cat "$response_file" >&2 || true
  echo "smoke-integration-gateway: missing signature response must use signature_invalid" >&2
  exit 1
fi

status="$(curl -sS -o "$response_file" -w "%{http_code}" \
  -X POST "http://127.0.0.1:18086/v1/provider-webhooks/gitlab" \
  -H "Content-Type: application/json" \
  --data '{"action":"ping","payload":"safe-smoke"}')"
if [[ "$status" != "400" ]]; then
  cat "$response_file" >&2 || true
  echo "smoke-integration-gateway: unsupported provider slug must be rejected with 400" >&2
  exit 1
fi
if ! grep -q '"code":"source_not_allowed"' "$response_file"; then
  cat "$response_file" >&2 || true
  echo "smoke-integration-gateway: unsupported provider response must use source_not_allowed" >&2
  exit 1
fi

echo "smoke-integration-gateway: HTTP edge boundary OK"
