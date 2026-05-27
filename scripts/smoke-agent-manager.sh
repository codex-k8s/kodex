#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/lib/smoke.sh"

kodex_smoke_init "smoke-agent-manager"
kodex_smoke_require_commands go kubectl curl grpcurl

KODEX_POSTGRES_IMAGE="$(kodex_postgres_image)"
KODEX_ACCESS_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_IMAGE KODEX_ACCESS_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/access-manager KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE KODEX_ACCESS_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/access-manager-migrations KODEX_ACCESS_MANAGER_VERSION access-manager)"
KODEX_PROJECT_CATALOG_IMAGE="$(kodex_image_from_repo KODEX_PROJECT_CATALOG_IMAGE KODEX_PROJECT_CATALOG_INTERNAL_IMAGE_REPOSITORY kodex/project-catalog KODEX_PROJECT_CATALOG_VERSION project-catalog)"
KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE KODEX_PROJECT_CATALOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/project-catalog-migrations KODEX_PROJECT_CATALOG_VERSION project-catalog)"
KODEX_PACKAGE_HUB_IMAGE="$(kodex_image_from_repo KODEX_PACKAGE_HUB_IMAGE KODEX_PACKAGE_HUB_INTERNAL_IMAGE_REPOSITORY kodex/package-hub KODEX_PACKAGE_HUB_VERSION package-hub)"
KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE KODEX_PACKAGE_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/package-hub-migrations KODEX_PACKAGE_HUB_VERSION package-hub)"
KODEX_PROVIDER_HUB_IMAGE="$(kodex_image_from_repo KODEX_PROVIDER_HUB_IMAGE KODEX_PROVIDER_HUB_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub KODEX_PROVIDER_HUB_VERSION provider-hub)"
KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE KODEX_PROVIDER_HUB_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/provider-hub-migrations KODEX_PROVIDER_HUB_VERSION provider-hub)"
KODEX_FLEET_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_FLEET_MANAGER_IMAGE KODEX_FLEET_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/fleet-manager KODEX_FLEET_MANAGER_VERSION fleet-manager)"
KODEX_FLEET_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_FLEET_MANAGER_MIGRATIONS_IMAGE KODEX_FLEET_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/fleet-manager-migrations KODEX_FLEET_MANAGER_VERSION fleet-manager)"
KODEX_RUNTIME_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_IMAGE KODEX_RUNTIME_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE KODEX_RUNTIME_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/runtime-manager-migrations KODEX_RUNTIME_MANAGER_VERSION runtime-manager)"
KODEX_AGENT_MANAGER_IMAGE="$(kodex_image_from_repo KODEX_AGENT_MANAGER_IMAGE KODEX_AGENT_MANAGER_INTERNAL_IMAGE_REPOSITORY kodex/agent-manager KODEX_AGENT_MANAGER_VERSION agent-manager)"
KODEX_AGENT_MANAGER_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_AGENT_MANAGER_MIGRATIONS_IMAGE KODEX_AGENT_MANAGER_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/agent-manager-migrations KODEX_AGENT_MANAGER_VERSION agent-manager)"
KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE="$(kodex_image_from_repo KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_INTERNAL_IMAGE_REPOSITORY kodex/platform-event-log-migrations KODEX_PLATFORM_EVENT_LOG_VERSION platform-event-log)"

required_runtime_values=(
  KODEX_POSTGRES_PASSWORD
  KODEX_ACCESS_MANAGER_DATABASE_DSN
  KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_PROJECT_CATALOG_DATABASE_DSN
  KODEX_PACKAGE_HUB_DATABASE_DSN
  KODEX_PACKAGE_HUB_EVENT_LOG_DATABASE_DSN
  KODEX_PROVIDER_HUB_DATABASE_DSN
  KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN
  KODEX_FLEET_MANAGER_DATABASE_DSN
  KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_RUNTIME_MANAGER_DATABASE_DSN
  KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_AGENT_MANAGER_DATABASE_DSN
  KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN
  KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN
)
if [[ "${KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PROJECT_CATALOG_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PROJECT_CATALOG_PROVIDER_HUB_BOOTSTRAP_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PACKAGE_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PACKAGE_HUB_ACCESS_CHECK_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PACKAGE_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_PROVIDER_HUB_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN)
fi
required_runtime_values+=(KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
if [[ "${KODEX_FLEET_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_FLEET_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_FLEET_MANAGER_ACCESS_CHECK_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_FLEET_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_RUNTIME_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_RUNTIME_MANAGER_ACCESS_CHECK_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN)
fi
required_runtime_values+=(KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_AUTH_TOKEN)
if [[ "${KODEX_AGENT_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_AGENT_MANAGER_PACKAGE_HUB_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN)
fi
if [[ "${KODEX_AGENT_MANAGER_PROVIDER_HUB_WRITE_ENABLED:-true}" == "true" ]]; then
  required_runtime_values+=(KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_AUTH_TOKEN)
fi
kodex_smoke_require_values "${required_runtime_values[@]}"

kodex_smoke_require_images \
  "KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE, KODEX_ACCESS_MANAGER_IMAGE, KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE, KODEX_PROJECT_CATALOG_IMAGE, KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE, KODEX_PACKAGE_HUB_IMAGE, KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE, KODEX_PROVIDER_HUB_IMAGE, KODEX_FLEET_MANAGER_MIGRATIONS_IMAGE, KODEX_FLEET_MANAGER_IMAGE, KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE, KODEX_RUNTIME_MANAGER_IMAGE, KODEX_AGENT_MANAGER_MIGRATIONS_IMAGE, KODEX_AGENT_MANAGER_IMAGE" \
  "$KODEX_POSTGRES_IMAGE" \
  "$KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_ACCESS_MANAGER_IMAGE" \
  "$KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE" \
  "$KODEX_PROJECT_CATALOG_IMAGE" \
  "$KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE" \
  "$KODEX_PACKAGE_HUB_IMAGE" \
  "$KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE" \
  "$KODEX_PROVIDER_HUB_IMAGE" \
  "$KODEX_FLEET_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_FLEET_MANAGER_IMAGE" \
  "$KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_RUNTIME_MANAGER_IMAGE" \
  "$KODEX_AGENT_MANAGER_MIGRATIONS_IMAGE" \
  "$KODEX_AGENT_MANAGER_IMAGE"

kodex_smoke_render \
  KODEX_POSTGRES_IMAGE \
  KODEX_ACCESS_MANAGER_IMAGE \
  KODEX_ACCESS_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PROJECT_CATALOG_IMAGE \
  KODEX_PROJECT_CATALOG_MIGRATIONS_IMAGE \
  KODEX_PACKAGE_HUB_IMAGE \
  KODEX_PACKAGE_HUB_MIGRATIONS_IMAGE \
  KODEX_PROVIDER_HUB_IMAGE \
  KODEX_PROVIDER_HUB_MIGRATIONS_IMAGE \
  KODEX_FLEET_MANAGER_IMAGE \
  KODEX_FLEET_MANAGER_MIGRATIONS_IMAGE \
  KODEX_RUNTIME_MANAGER_IMAGE \
  KODEX_RUNTIME_MANAGER_MIGRATIONS_IMAGE \
  KODEX_AGENT_MANAGER_IMAGE \
  KODEX_AGENT_MANAGER_MIGRATIONS_IMAGE \
  KODEX_PLATFORM_EVENT_LOG_MIGRATIONS_IMAGE

kodex_smoke_apply_foundation
kodex_smoke_apply_migrations access-manager access-manager-migrations
kodex_smoke_apply_deployment access-manager access-manager/access-manager.yaml
kodex_smoke_apply_migrations provider-hub provider-hub-migrations
kodex_smoke_apply_deployment provider-hub provider-hub/provider-hub.yaml
kodex_smoke_apply_migrations project-catalog project-catalog-migrations
kodex_smoke_apply_deployment project-catalog project-catalog/project-catalog.yaml
kodex_smoke_apply_migrations package-hub package-hub-migrations
kodex_smoke_apply_deployment package-hub package-hub/package-hub.yaml
kodex_smoke_apply_migrations fleet-manager fleet-manager-migrations
kodex_smoke_apply_deployment fleet-manager fleet-manager/fleet-manager.yaml
kodex_smoke_apply_migrations runtime-manager runtime-manager-migrations
kodex_smoke_apply_deployment runtime-manager runtime-manager/runtime-manager.yaml
kodex_smoke_apply_migrations agent-manager agent-manager-migrations
kodex_smoke_apply_deployment agent-manager agent-manager/agent-manager.yaml
kodex_smoke_check_readyz agent-manager 18087

kodex_smoke_start_port_forward "svc/agent-manager" "19097:9090"
grpc_payload='{"meta":{"actor":{"type":"service","id":"smoke-agent-manager"},"request_id":"smoke-agent-manager","request_context":{"source":"smoke-agent-manager"}},"page":{"page_size":1}}'
call_agent_manager() {
  local token="$1"
  local grpc_headers=(
    -H "x-kodex-caller-type: service"
    -H "x-kodex-caller-id: smoke-agent-manager"
  )
  if [[ -n "$token" ]]; then
    grpc_headers+=(-H "authorization: Bearer ${token}")
  fi

  grpcurl \
    -plaintext \
    -import-path "${PROJECT_ROOT}/proto" \
    -proto "kodex/agents/v1/agent_manager.proto" \
    "${grpc_headers[@]}" \
    -d "$grpc_payload" \
    127.0.0.1:19097 \
    kodex.agents.v1.AgentManagerService/ListAgentRuns
}

positive_output=""
for _ in $(seq 1 30); do
  positive_output="$(call_agent_manager "${KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN:-}" 2>&1)" && positive_status="ok" || positive_status="$?"
  if [[ "$positive_status" == "ok" ]] || grep -Eq "Code: PermissionDenied" <<<"$positive_output"; then
    positive_status="accepted"
    break
  fi
  sleep 1
done

if [[ "$positive_status" != "accepted" ]]; then
  cat "$KODEX_SMOKE_LAST_PORT_FORWARD_LOG" >&2 || true
  printf '%s\n' "$positive_output" >&2
  echo "smoke-agent-manager: gRPC boundary did not accept the configured agent-manager token" >&2
  exit 1
fi

if [[ "${KODEX_AGENT_MANAGER_GRPC_AUTH_REQUIRED:-true}" == "true" ]]; then
  negative_output="$(call_agent_manager "invalid-smoke-token" 2>&1)" && negative_status="ok" || negative_status="$?"
  if [[ "$negative_status" == "ok" ]] || ! grep -Eq "Code: Unauthenticated" <<<"$negative_output"; then
    printf '%s\n' "$negative_output" >&2
    echo "smoke-agent-manager: invalid agent-manager token must be rejected with Unauthenticated" >&2
    exit 1
  fi
fi

echo "smoke-agent-manager: gRPC boundary OK"
