#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${KODEX_PROVIDER_LIVE_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
fi

APPLY="${KODEX_PROVIDER_LIVE_SMOKE_APPLY:-false}"
ORG="${KODEX_PROVIDER_LIVE_SMOKE_ORG:-codex-k8s}"
KIND="${KODEX_PROVIDER_LIVE_SMOKE_KIND:-bootstrap}"
DATE_SLUG="$(date -u +%Y%m%d)"
TIME_SLUG="$(date -u +%H%M%S)"
REPO="${KODEX_PROVIDER_LIVE_SMOKE_REPO:-kodex-smoke-provider-${DATE_SLUG}-${TIME_SLUG}}"
BRANCH="${KODEX_PROVIDER_LIVE_SMOKE_BRANCH:-kodex/live-smoke-${KIND}-${DATE_SLUG}-${TIME_SLUG}}"
BASE_BRANCH="${KODEX_PROVIDER_LIVE_SMOKE_BASE_BRANCH:-}"
FILE_PATH="${KODEX_PROVIDER_LIVE_SMOKE_FILE_PATH:-kodex-live-smoke/${KIND}.txt}"
GATEWAY_URL="${KODEX_PROVIDER_LIVE_SMOKE_GATEWAY_URL:-}"
WEBHOOK_SECRET="${KODEX_PROVIDER_LIVE_SMOKE_WEBHOOK_SECRET:-${KODEX_GITHUB_WEBHOOK_SECRET:-}}"
PROVIDER_HUB_ADDR="${KODEX_PROVIDER_LIVE_SMOKE_PROVIDER_HUB_GRPC_ADDR:-}"
PROVIDER_HUB_TOKEN="${KODEX_PROVIDER_LIVE_SMOKE_PROVIDER_HUB_GRPC_TOKEN:-${KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN:-}}"
EXPECT_SIGNAL="${KODEX_PROVIDER_LIVE_SMOKE_EXPECT_SIGNAL:-false}"
DELIVERY_ID="${KODEX_PROVIDER_LIVE_SMOKE_DELIVERY_ID:-}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/smoke-provider-github-live.sh [--apply] [--org codex-k8s] [--repo name] [--kind bootstrap|adoption]

По умолчанию выполняется dry-run без изменений в GitHub.

Основные env:
  KODEX_PROVIDER_LIVE_SMOKE_APPLY=true        Разрешить реальные изменения в GitHub.
  KODEX_PROVIDER_LIVE_SMOKE_ORG=codex-k8s     GitHub organization.
  KODEX_PROVIDER_LIVE_SMOKE_REPO=...          Тестовый репозиторий для создания или переиспользования.
  KODEX_PROVIDER_LIVE_SMOKE_KIND=bootstrap    Тип onboarding-сигнала: bootstrap или adoption.
  KODEX_GITHUB_PAT=...                        Токен GitHub для live GitHub операций.

Опциональные runtime-проверки после создания merged PR:
  KODEX_PROVIDER_LIVE_SMOKE_GATEWAY_URL=http://127.0.0.1:18086
  KODEX_PROVIDER_LIVE_SMOKE_WEBHOOK_SECRET=...
  KODEX_PROVIDER_LIVE_SMOKE_PROVIDER_HUB_GRPC_ADDR=127.0.0.1:19095
  KODEX_PROVIDER_LIVE_SMOKE_EXPECT_SIGNAL=true

Скрипт не удаляет репозитории. Очистка выполняется вручную только после отдельного решения владельца.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply)
      APPLY=true
      shift
      ;;
    --dry-run)
      APPLY=false
      shift
      ;;
    --org)
      ORG="${2:?--org requires value}"
      shift 2
      ;;
    --repo)
      REPO="${2:?--repo requires value}"
      shift 2
      ;;
    --kind)
      KIND="${2:?--kind requires value}"
      shift 2
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "${KODEX_PROVIDER_LIVE_SMOKE_BRANCH:-}" ]]; then
  BRANCH="kodex/live-smoke-${KIND}-${DATE_SLUG}-${TIME_SLUG}"
fi
if [[ -z "${KODEX_PROVIDER_LIVE_SMOKE_FILE_PATH:-}" ]]; then
  FILE_PATH="kodex-live-smoke/${KIND}.txt"
fi

case "$KIND" in
  bootstrap|adoption)
    ;;
  *)
    echo "smoke-provider-github-live: KODEX_PROVIDER_LIVE_SMOKE_KIND должен быть bootstrap или adoption" >&2
    exit 1
    ;;
esac

is_true() {
  case "${1:-}" in
    true|TRUE|1|yes|YES)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

require_commands() {
  local command_name
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "smoke-provider-github-live: требуется команда ${command_name}" >&2
      exit 1
    fi
  done
}

github_token() {
  printf '%s' "${KODEX_PROVIDER_LIVE_SMOKE_GITHUB_TOKEN:-${KODEX_GITHUB_PAT:-${CODEXK8S_GITHUB_PAT:-}}}"
}

github_api() {
  GH_TOKEN="$GITHUB_TOKEN" gh api "$@"
}

github_pr() {
  GH_TOKEN="$GITHUB_TOKEN" gh pr "$@"
}

print_plan() {
  echo "smoke-provider-github-live: режим dry-run, изменений в GitHub нет"
  echo "smoke-provider-github-live: GitHub org=${ORG}, repo=${REPO}, kind=${KIND}, branch=${BRANCH}"
  echo "smoke-provider-github-live: план: проверить токен и организацию, создать или переиспользовать репозиторий, создать ветку/PR, выполнить merge PR, собрать safe pull_request closed+merged payload"
  if [[ -n "$GATEWAY_URL" ]]; then
    echo "smoke-provider-github-live: план: отправить payload в integration-gateway ${GATEWAY_URL%/}/v1/provider-webhooks/github при наличии webhook secret"
  fi
  if [[ -n "$PROVIDER_HUB_ADDR" ]]; then
    echo "smoke-provider-github-live: план: проверить provider-hub gRPC boundary по адресу ${PROVIDER_HUB_ADDR}"
  fi
  echo "smoke-provider-github-live: для реальных изменений задайте --apply или KODEX_PROVIDER_LIVE_SMOKE_APPLY=true"
}

check_read_only_access() {
  if [[ -z "$GITHUB_TOKEN" ]]; then
    echo "smoke-provider-github-live: GitHub token не задан; dry-run завершён без обращения к GitHub"
    return
  fi
  if ! command -v gh >/dev/null 2>&1; then
    echo "smoke-provider-github-live: gh не найден; dry-run завершён без обращения к GitHub"
    return
  fi
  local login
  login="$(github_api user --jq .login)"
  github_api "orgs/${ORG}" --jq .login >/dev/null
  echo "smoke-provider-github-live: GitHub token доступен для login=${login}; организация ${ORG} доступна"
}

ensure_repo() {
  local repo_file="$1"
  if github_api "repos/${ORG}/${REPO}" >"$repo_file" 2>/dev/null; then
    echo "smoke-provider-github-live: переиспользуется репозиторий https://github.com/${ORG}/${REPO}"
    return
  fi
  echo "smoke-provider-github-live: создаётся приватный тестовый репозиторий ${ORG}/${REPO}"
  github_api \
    -X POST "orgs/${ORG}/repos" \
    -F "name=${REPO}" \
    -F "private=true" \
    -F "auto_init=true" \
    -f "description=Kodex provider live smoke repository; safe to remove manually after owner approval." >"$repo_file"
}

refresh_repo() {
  local repo_file="$1"
  for _ in $(seq 1 30); do
    github_api "repos/${ORG}/${REPO}" >"$repo_file"
    if [[ "$(jq -r '.default_branch // empty' "$repo_file")" != "" ]]; then
      return
    fi
    sleep 1
  done
  echo "smoke-provider-github-live: GitHub не вернул default branch для ${ORG}/${REPO}" >&2
  exit 1
}

ensure_branch() {
  local repo_file="$1"
  local branch_file="$2"
  local default_branch
  default_branch="$(jq -r '.default_branch // empty' "$repo_file")"
  if [[ -z "$BASE_BRANCH" ]]; then
    BASE_BRANCH="$default_branch"
  fi
  local base_ref_file
  base_ref_file="$(mktemp)"
  github_api "repos/${ORG}/${REPO}/git/ref/heads/${BASE_BRANCH}" >"$base_ref_file"
  local base_sha
  base_sha="$(jq -r '.object.sha' "$base_ref_file")"
  rm -f "$base_ref_file"

  if github_api "repos/${ORG}/${REPO}/git/ref/heads/${BRANCH}" >"$branch_file" 2>/dev/null; then
    echo "smoke-provider-github-live: переиспользуется branch ${BRANCH}"
    return
  fi
  echo "smoke-provider-github-live: создаётся branch ${BRANCH} от ${BASE_BRANCH}"
  github_api \
    -X POST "repos/${ORG}/${REPO}/git/refs" \
    -f "ref=refs/heads/${BRANCH}" \
    -f "sha=${base_sha}" >"$branch_file"
}

put_smoke_file() {
  local existing_file
  local content_file
  local encoded
  existing_file="$(mktemp)"
  content_file="$(mktemp)"
  cat >"$content_file" <<EOF
Kodex provider live smoke
kind=${KIND}
repo=${ORG}/${REPO}
branch=${BRANCH}
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
  encoded="$(base64 -w0 "$content_file")"
  rm -f "$content_file"

  local args=(
    -X PUT "repos/${ORG}/${REPO}/contents/${FILE_PATH}"
    -f "message=Kodex provider live smoke"
    -f "content=${encoded}"
    -f "branch=${BRANCH}"
  )
  if github_api "repos/${ORG}/${REPO}/contents/${FILE_PATH}?ref=${BRANCH}" >"$existing_file" 2>/dev/null; then
    args+=(-f "sha=$(jq -r '.sha' "$existing_file")")
  fi
  rm -f "$existing_file"
  github_api "${args[@]}" >/dev/null
  echo "smoke-provider-github-live: обновлён безопасный файл ${FILE_PATH}"
}

watermark_body() {
  local work_type="repository_bootstrap"
  if [[ "$KIND" == "adoption" ]]; then
    work_type="repository_adoption"
  fi
  cat <<EOF
<!-- kodex:artifact v1
kind: pull_request
managed_by: kodex
work_type: ${work_type}
source_ref: ${BRANCH}
provider_operation_ref: provider-hub:operation:live-smoke-${REPO}
-->
Provider live-smoke PR for ${KIND}. Payload is generated locally and not printed.
EOF
}

ensure_pull_request() {
  local pr_file="$1"
  local open_pr
  open_pr="$(github_pr list -R "${ORG}/${REPO}" --state open --head "$BRANCH" --json number --jq '.[0].number // empty')"
  if [[ -n "$open_pr" ]]; then
    github_api "repos/${ORG}/${REPO}/pulls/${open_pr}" >"$pr_file"
    echo "smoke-provider-github-live: переиспользуется открытый PR #${open_pr}"
    return
  fi
  local body_file
  body_file="$(mktemp)"
  watermark_body >"$body_file"
  github_api \
    -X POST "repos/${ORG}/${REPO}/pulls" \
    -f "title=Kodex provider live smoke ${KIND}" \
    -f "head=${BRANCH}" \
    -f "base=${BASE_BRANCH}" \
    -f "body=@${body_file}" >"$pr_file"
  rm -f "$body_file"
  echo "smoke-provider-github-live: создан PR #$(jq -r '.number' "$pr_file")"
}

merge_pull_request() {
  local pr_file="$1"
  local pr_number
  pr_number="$(jq -r '.number' "$pr_file")"
  if [[ "$(jq -r '.merged // false' "$pr_file")" == "true" ]]; then
    echo "smoke-provider-github-live: PR #${pr_number} уже merged"
    return
  fi
  for _ in $(seq 1 30); do
    github_api "repos/${ORG}/${REPO}/pulls/${pr_number}" >"$pr_file"
    case "$(jq -r '.mergeable' "$pr_file")" in
      true)
        break
        ;;
      false)
        echo "smoke-provider-github-live: PR #${pr_number} не готов к merge" >&2
        exit 1
        ;;
    esac
    sleep 1
  done
  github_api \
    -X PUT "repos/${ORG}/${REPO}/pulls/${pr_number}/merge" \
    -f "merge_method=squash" \
    -f "commit_title=Kodex provider live smoke ${KIND}" >/dev/null
  github_api "repos/${ORG}/${REPO}/pulls/${pr_number}" >"$pr_file"
  echo "smoke-provider-github-live: PR #${pr_number} merged"
}

build_payload() {
  local repo_file="$1"
  local pr_file="$2"
  local payload_file="$3"
  jq -n --slurpfile repo "$repo_file" --slurpfile pr "$pr_file" '
    {
      action: "closed",
      repository: {
        id: $repo[0].id,
        full_name: $repo[0].full_name
      },
      pull_request: {
        id: $pr[0].id,
        number: $pr[0].number,
        html_url: $pr[0].html_url,
        title: $pr[0].title,
        state: $pr[0].state,
        body: $pr[0].body,
        labels: (($pr[0].labels // []) | map({name})),
        assignees: (($pr[0].assignees // []) | map({login})),
        merged: $pr[0].merged,
        merge_commit_sha: $pr[0].merge_commit_sha,
        base: {
          ref: $pr[0].base.ref,
          sha: $pr[0].base.sha
        },
        head: {
          ref: $pr[0].head.ref,
          sha: $pr[0].head.sha
        },
        merged_at: $pr[0].merged_at,
        closed_at: $pr[0].closed_at,
        updated_at: $pr[0].updated_at
      }
    }' >"$payload_file"
}

github_signature() {
  local secret="$1"
  local payload_file="$2"
  KODEX_PROVIDER_LIVE_SMOKE_WEBHOOK_SECRET_VALUE="$secret" python3 - "$payload_file" <<'PY'
import hashlib
import hmac
import os
import sys

secret = os.environ["KODEX_PROVIDER_LIVE_SMOKE_WEBHOOK_SECRET_VALUE"].encode("utf-8")
with open(sys.argv[1], "rb") as payload:
    body = payload.read()
print("sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest())
PY
}

post_gateway() {
  local payload_file="$1"
  local delivery_id="$2"
  if [[ -z "$GATEWAY_URL" ]]; then
    echo "smoke-provider-github-live: проверка integration-gateway пропущена; задайте KODEX_PROVIDER_LIVE_SMOKE_GATEWAY_URL"
    return
  fi
  if [[ -z "$WEBHOOK_SECRET" ]]; then
    echo "smoke-provider-github-live: проверка integration-gateway пропущена; webhook secret не задан"
    return
  fi
  require_commands curl python3
  local response_file
  response_file="$(mktemp)"
  local signature
  signature="$(github_signature "$WEBHOOK_SECRET" "$payload_file")"
  local status
  status="$(curl -sS -o "$response_file" -w "%{http_code}" \
    -X POST "${GATEWAY_URL%/}/v1/provider-webhooks/github" \
    -H "Content-Type: application/json" \
    -H "X-GitHub-Delivery: ${delivery_id}" \
    -H "X-GitHub-Event: pull_request" \
    -H "X-Hub-Signature-256: ${signature}" \
    --data-binary "@${payload_file}")"
  if [[ "$status" != "202" ]]; then
    local safe_code
    safe_code="$(jq -r '.error.code // .code // empty' "$response_file" 2>/dev/null || true)"
    rm -f "$response_file"
    echo "smoke-provider-github-live: integration-gateway вернул HTTP ${status}${safe_code:+ code=${safe_code}}" >&2
    exit 1
  fi
  rm -f "$response_file"
  echo "smoke-provider-github-live: integration-gateway принял подписанный live payload"
}

provider_hub_headers() {
  local headers=(
    -H "x-kodex-caller-type: service"
    -H "x-kodex-caller-id: smoke-provider-github-live"
  )
  if [[ -n "$PROVIDER_HUB_TOKEN" ]]; then
    headers+=(-H "authorization: Bearer ${PROVIDER_HUB_TOKEN}")
  fi
  printf '%s\n' "${headers[@]}"
}

check_provider_hub_boundary() {
  if [[ -z "$PROVIDER_HUB_ADDR" ]]; then
    echo "smoke-provider-github-live: проверка provider-hub gRPC пропущена; задайте KODEX_PROVIDER_LIVE_SMOKE_PROVIDER_HUB_GRPC_ADDR"
    return
  fi
  require_commands grpcurl
  local output_file
  output_file="$(mktemp)"
  mapfile -t headers < <(provider_hub_headers)
  local payload='{"meta":{"actor":{"type":"service","id":"smoke-provider-github-live"},"request_id":"smoke-provider-github-live","request_context":{"source":"smoke-provider-github-live"}},"page":{"page_size":1}}'
  if grpcurl \
    -plaintext \
    -proto "${PROJECT_ROOT}/proto/kodex/providers/v1/provider_hub.proto" \
    "${headers[@]}" \
    -d "$payload" \
    "$PROVIDER_HUB_ADDR" \
    kodex.providers.v1.ProviderHubService/ListProviderOperations >"$output_file" 2>&1; then
    rm -f "$output_file"
    echo "smoke-provider-github-live: provider-hub gRPC boundary OK"
    return
  fi
  if grep -q "Code: PermissionDenied" "$output_file"; then
    rm -f "$output_file"
    echo "smoke-provider-github-live: provider-hub gRPC boundary OK; доменное право ожидаемо отклонено"
    return
  fi
  cat "$output_file" >&2
  rm -f "$output_file"
  echo "smoke-provider-github-live: provider-hub gRPC boundary недоступен" >&2
  exit 1
}

check_merge_signal() {
  local signal_key="$1"
  if [[ -z "$PROVIDER_HUB_ADDR" ]]; then
    return
  fi
  if ! is_true "$EXPECT_SIGNAL"; then
    echo "smoke-provider-github-live: проверка read surface merge signal пропущена; включите KODEX_PROVIDER_LIVE_SMOKE_EXPECT_SIGNAL=true после подготовки PR projection/binding в provider-hub"
    return
  fi
  require_commands grpcurl jq
  mapfile -t headers < <(provider_hub_headers)
  local output_file
  output_file="$(mktemp)"
  local grpc_payload
  grpc_payload="{\"signal_key\":\"${signal_key}\",\"meta\":{\"actor\":{\"type\":\"service\",\"id\":\"smoke-provider-github-live\"},\"request_id\":\"smoke-provider-github-live\",\"request_context\":{\"source\":\"smoke-provider-github-live\"}}}"
  grpcurl \
    -plaintext \
    -proto "${PROJECT_ROOT}/proto/kodex/providers/v1/provider_hub.proto" \
    "${headers[@]}" \
    -d "$grpc_payload" \
    "$PROVIDER_HUB_ADDR" \
    kodex.providers.v1.ProviderHubService/GetRepositoryMergeSignal >"$output_file"
  if [[ "$(jq -r '.readStatus // empty' "$output_file")" != "PROVIDER_OWNED_DATA_STATUS_READY" ]]; then
    rm -f "$output_file"
    echo "smoke-provider-github-live: RepositoryMergeSignal не готов; проверьте provider-hub PR projection, binding и watermark precondition" >&2
    exit 1
  fi
  rm -f "$output_file"
  echo "smoke-provider-github-live: provider-hub RepositoryMergeSignal read surface OK"
}

run_apply() {
  require_commands gh jq python3 base64
  if [[ -z "$GITHUB_TOKEN" ]]; then
    echo "smoke-provider-github-live: для --apply нужен KODEX_GITHUB_PAT или KODEX_PROVIDER_LIVE_SMOKE_GITHUB_TOKEN" >&2
    exit 1
  fi
  local login
  login="$(github_api user --jq .login)"
  github_api "orgs/${ORG}" --jq .login >/dev/null
  echo "smoke-provider-github-live: GitHub доступен для login=${login}, организация=${ORG}"

  local repo_file branch_file pr_file payload_file
  repo_file="$(mktemp)"
  branch_file="$(mktemp)"
  pr_file="$(mktemp)"
  payload_file="$(mktemp)"
  trap "rm -f '$repo_file' '$branch_file' '$pr_file' '$payload_file'" EXIT

  ensure_repo "$repo_file"
  refresh_repo "$repo_file"
  ensure_branch "$repo_file" "$branch_file"
  put_smoke_file
  ensure_pull_request "$pr_file"
  merge_pull_request "$pr_file"
  refresh_repo "$repo_file"
  build_payload "$repo_file" "$pr_file" "$payload_file"

  local pr_number repository_id full_name signal_key delivery_id
  pr_number="$(jq -r '.number' "$pr_file")"
  repository_id="$(jq -r '.id' "$repo_file")"
  full_name="$(jq -r '.full_name' "$repo_file")"
  signal_key="provider:github:repository_merge:${KIND}:github:${full_name}:pull_request:${pr_number}"
  delivery_id="${DELIVERY_ID:-live-smoke-${REPO}-${pr_number}-$(date -u +%s)}"

  echo "smoke-provider-github-live: live GitHub path готов"
  echo "smoke-provider-github-live: repo=https://github.com/${full_name}, provider_repository_id=${repository_id}, pr_number=${pr_number}, signal_key=${signal_key}"
  echo "smoke-provider-github-live: ручная очистка после отдельного решения владельца: gh repo delete ${full_name} --confirm"

  post_gateway "$payload_file" "$delivery_id"
  check_provider_hub_boundary
  check_merge_signal "$signal_key"
}

GITHUB_TOKEN="$(github_token)"

if ! is_true "$APPLY"; then
  print_plan
  check_read_only_access
  exit 0
fi

run_apply
