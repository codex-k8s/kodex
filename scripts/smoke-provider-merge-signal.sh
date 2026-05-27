#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

FIXTURE_PATH="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_FIXTURE:-${PROJECT_ROOT}/fixtures/provider-webhooks/github_pull_request_bootstrap_merged.json}"
MODE="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_MODE:-fixture}"
SIGNAL_KEY="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_SIGNAL_KEY:-provider:github:repository_merge:bootstrap:github:kodex-smoke/repository:pull_request:88}"
DELIVERY_ID="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_DELIVERY_ID:-smoke-bootstrap-merged}"

require_commands() {
  local command_name
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "smoke-provider-merge-signal: ${command_name} is required" >&2
      exit 1
    fi
  done
}

usage() {
  cat <<'USAGE'
Usage:
  scripts/smoke-provider-merge-signal.sh
  KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_MODE=live-http scripts/smoke-provider-merge-signal.sh

Modes:
  fixture    Hermetic staged smoke. Runs the safe GitHub pull_request merged fixture through
             integration-gateway route tests and provider-hub domain/read/outbox tests.
             No live secret, real domain, Kubernetes cluster, or provider API is required.

  live-http  Sends the same fixture to a running integration-gateway and reads the resulting
             RepositoryMergeSignal from provider-hub through gRPC. This mode requires a
             configured webhook secret and an existing bootstrap/adoption PR projection in
             provider-hub. Set KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_CHECK_EVENT_LOG=true and
             KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_EVENT_LOG_DSN to verify platform-event-log.
USAGE
}

run_fixture_mode() {
  require_commands go
  echo "smoke-provider-merge-signal: fixture path ${FIXTURE_PATH#"$PROJECT_ROOT"/}"
  go test ./services/internal/provider-hub/internal/domain/service -run TestSmokeFixtureGitHubBootstrapMergeSignalPath -count=1
  go test ./services/external/integration-gateway/internal/transport/http -run TestProviderWebhookForwardsGitHubPullRequestMergedFixture -count=1
  echo "smoke-provider-merge-signal: fixture/domain/edge path OK"
}

source_optional_env() {
  local env_file="${KODEX_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
  if [[ -f "$env_file" ]]; then
    # shellcheck disable=SC1090
    source "$env_file"
  fi
}

github_signature() {
  local secret="$1"
  KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_WEBHOOK_SECRET_VALUE="$secret" python3 - "$FIXTURE_PATH" <<'PY'
import hashlib
import hmac
import os
import sys

secret = os.environ["KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_WEBHOOK_SECRET_VALUE"].encode("utf-8")
with open(sys.argv[1], "rb") as fixture:
    payload = fixture.read()
print("sha256=" + hmac.new(secret, payload, hashlib.sha256).hexdigest())
PY
}

verify_grpc_signal() {
  python3 - "$FIXTURE_PATH" "$1" "$SIGNAL_KEY" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fixture_file:
    fixture = json.load(fixture_file)
with open(sys.argv[2], "r", encoding="utf-8") as response_file:
    response = json.load(response_file)
signal_key = sys.argv[3]

signal = response.get("mergeSignal") or {}
pull_request = fixture["pull_request"]
repository = fixture["repository"]

checks = {
    "signalKey": signal_key,
    "kind": "REPOSITORY_MERGE_SIGNAL_KIND_BOOTSTRAP",
    "providerSlug": "github",
    "repositoryFullName": repository["full_name"],
    "baseBranch": pull_request["base"]["ref"],
    "headBranch": pull_request["head"]["ref"],
    "mergeCommitSha": pull_request["merge_commit_sha"],
    "sourceRef": pull_request["head"]["ref"],
    "status": "REPOSITORY_MERGE_SIGNAL_STATUS_MERGED",
}
if response.get("readStatus") != "PROVIDER_OWNED_DATA_STATUS_READY":
    raise SystemExit("RepositoryMergeSignal read_status is not READY")
for field, expected in checks.items():
    if signal.get(field) != expected:
        raise SystemExit(f"RepositoryMergeSignal {field}={signal.get(field)!r}, want {expected!r}")
if str(signal.get("pullRequestNumber")) != str(pull_request["number"]):
    raise SystemExit("RepositoryMergeSignal pullRequestNumber mismatch")
for field in ("signalId", "mergedAt", "observedAt", "version", "etag"):
    if not signal.get(field):
        raise SystemExit(f"RepositoryMergeSignal misses {field}")
PY
}

check_event_log() {
  local event_log_dsn="$1"
  local event_log_source="$2"
  require_commands psql
  local count
  for _ in $(seq 1 30); do
    count="$(PGCONNECT_TIMEOUT=5 psql "$event_log_dsn" -v signal_key="$SIGNAL_KEY" -v source_service="$event_log_source" -Atqc "SELECT count(*) FROM platform_event_log WHERE source_service = :'source_service' AND event_type IN ('provider.repository.bootstrap_merged', 'provider.repository.adoption_merged') AND payload->>'signal_key' = :'signal_key';")"
    if [[ "${count:-0}" != "0" ]]; then
      echo "smoke-provider-merge-signal: platform-event-log producer event OK"
      return
    fi
    sleep 1
  done
  echo "smoke-provider-merge-signal: merge signal event was not observed in platform-event-log" >&2
  exit 1
}

run_live_http_mode() {
  require_commands curl grpcurl python3
  source_optional_env

  local gateway_url="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_GATEWAY_URL:-http://127.0.0.1:18086}"
  local provider_hub_addr="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_PROVIDER_HUB_GRPC_ADDR:-127.0.0.1:19095}"
  local webhook_secret="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_WEBHOOK_SECRET:-${KODEX_GITHUB_WEBHOOK_SECRET:-}}"
  local provider_hub_token="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_PROVIDER_HUB_GRPC_TOKEN:-${KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN:-}}"
  local check_event_log_mode="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_CHECK_EVENT_LOG:-false}"
  local event_log_dsn="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_EVENT_LOG_DSN:-${KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN:-}}"
  local event_log_source="${KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_EVENT_LOG_SOURCE:-${KODEX_PROVIDER_HUB_OUTBOX_EVENT_LOG_SOURCE:-provider-hub}}"

  if [[ -z "$webhook_secret" ]]; then
    echo "smoke-provider-merge-signal: live-http mode requires a configured webhook secret" >&2
    echo "smoke-provider-merge-signal: run fixture mode for the no-secret staged smoke" >&2
    exit 1
  fi

  local response_file grpc_output_file
  response_file="$(mktemp)"
  grpc_output_file="$(mktemp)"
  trap 'rm -f "$response_file" "$grpc_output_file"' EXIT

  local signature
  signature="$(github_signature "$webhook_secret")"
  local status
  status="$(curl -sS -o "$response_file" -w "%{http_code}" \
    -X POST "${gateway_url%/}/v1/provider-webhooks/github" \
    -H "Content-Type: application/json" \
    -H "X-GitHub-Delivery: ${DELIVERY_ID}" \
    -H "X-GitHub-Event: pull_request" \
    -H "X-Hub-Signature-256: ${signature}" \
    --data-binary "@${FIXTURE_PATH}")"
  if [[ "$status" != "202" ]]; then
    cat "$response_file" >&2 || true
    echo "smoke-provider-merge-signal: integration-gateway did not accept the signed fixture" >&2
    exit 1
  fi

  local grpc_headers=(
    -H "x-kodex-caller-type: service"
    -H "x-kodex-caller-id: smoke-provider-merge-signal"
  )
  if [[ -n "$provider_hub_token" ]]; then
    grpc_headers+=(-H "authorization: Bearer ${provider_hub_token}")
  fi
  local grpc_payload
  grpc_payload="{\"signal_key\":\"${SIGNAL_KEY}\",\"meta\":{\"actor\":{\"type\":\"service\",\"id\":\"smoke-provider-merge-signal\"},\"request_id\":\"smoke-provider-merge-signal\",\"request_context\":{\"source\":\"smoke-provider-merge-signal\"}}}"
  grpcurl \
    -plaintext \
    -proto "${PROJECT_ROOT}/proto/kodex/providers/v1/provider_hub.proto" \
    "${grpc_headers[@]}" \
    -d "$grpc_payload" \
    "$provider_hub_addr" \
    kodex.providers.v1.ProviderHubService/GetRepositoryMergeSignal >"$grpc_output_file"

  verify_grpc_signal "$grpc_output_file"
  echo "smoke-provider-merge-signal: live HTTP -> provider-hub read surface OK"

  if [[ "$check_event_log_mode" == "true" ]]; then
    if [[ -z "$event_log_dsn" ]]; then
      echo "smoke-provider-merge-signal: event-log DSN is required when event-log check is enabled" >&2
      exit 1
    fi
    check_event_log "$event_log_dsn" "$event_log_source"
  else
    echo "smoke-provider-merge-signal: platform-event-log check skipped; set KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_CHECK_EVENT_LOG=true to enable it"
  fi
}

case "$MODE" in
  fixture)
    run_fixture_mode
    ;;
  live-http)
    run_live_http_mode
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
