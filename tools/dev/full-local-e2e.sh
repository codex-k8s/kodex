#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex full local E2E failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 [--check] [--skip-build] --context <exact-context>" \
    '  [--kubeconfig <path>] [--state-directory <path>]' \
    '  [--cluster-marker <root-owned-path>] [--expected-sha <40-hex-commit>]' \
    '  [--resource-prefix <slug>] [--run-timeout-ms <milliseconds>]' \
    '  [--batch browser|integration|role-image|archive|backup|hot-reload]...' \
    '  [--target <test-make-target>]...' >&2
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
kubeconfig=${KODEX_DEV_KUBECONFIG:-"$HOME/.kube/kodex-dev-local"}
context=""
state_directory="$repository_root/.kodex-dev"
resource_prefix="full-local-e2e-$(date -u +%Y%m%d%H%M%S)"
run_timeout_ms=900000
cluster_marker=""
expected_sha=""
check_only=false
skip_build=false
targets=()
batches=()
canonical_batches=(hot-reload browser integration role-image archive backup)
declare -A selected_batches=()

while (($# > 0)); do
  case "$1" in
    --check) check_only=true; shift ;;
    --skip-build) skip_build=true; shift ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --run-timeout-ms) run_timeout_ms=${2:-}; shift 2 ;;
    --cluster-marker) cluster_marker=${2:-}; shift 2 ;;
    --expected-sha) expected_sha=${2:-}; shift 2 ;;
    --batch)
      batch=${2:-}
      case "$batch" in
        browser|integration|role-image|archive|backup|hot-reload) ;;
        *) usage; fail "unsupported E2E batch: $batch" ;;
      esac
      [[ -z "${selected_batches[$batch]:-}" ]] || fail "E2E batch is duplicated: $batch"
      selected_batches[$batch]=1
      batches+=("$batch")
      shift 2
      ;;
    --target) targets+=("${2:-}"); shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

if ((${#batches[@]} == 0)); then
  for batch in "${canonical_batches[@]}"; do
    selected_batches[$batch]=1
  done
fi

batch_selected() {
  [[ -n "${selected_batches[$1]:-}" ]]
}

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
[[ -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is absent or unsafe'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" &&
  ! -L "$state_directory" ]] || fail 'state directory must be an exact safe absolute path'
[[ "$resource_prefix" =~ ^[a-z0-9]([a-z0-9-]{2,38}[a-z0-9])$ ]] ||
  fail 'E2E resource prefix must be a lowercase 4-40 character slug'
[[ "$run_timeout_ms" =~ ^[0-9]+$ && "$run_timeout_ms" -ge 60000 &&
  "$run_timeout_ms" -le 1800000 ]] ||
  fail 'E2E run timeout must be between 60000 and 1800000 milliseconds'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
if [[ -n "$expected_sha" ]]; then
  [[ "$expected_sha" =~ ^[a-f0-9]{40}$ &&
    "$(git -C "$repository_root" rev-parse HEAD)" == "$expected_sha" ]] ||
    fail 'source HEAD does not match the expected SHA'
  [[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] ||
    fail 'acceptance E2E requires a clean source checkout'
fi
for command_name in bash date jq kubectl make npm; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

declare -A unique_targets=()
for target in "${targets[@]}"; do
  [[ "$target" =~ ^test-[a-z0-9][a-z0-9_.-]{0,62}$ ]] ||
    fail "additional target must be a safe test-* Make target: $target"
  [[ -z "${unique_targets[$target]:-}" ]] || fail "additional target is duplicated: $target"
  unique_targets[$target]=1
done

export KUBECONFIG="$kubeconfig"
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

namespace_state=$(kubectl get namespace/kodex-system -o json 2>/dev/null || true)
if [[ -n "$namespace_state" ]]; then
  jq -e '
    .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
    .metadata.labels["kodex.dev/environment"] == "staging" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
  ' <<<"$namespace_state" >/dev/null || fail 'existing Kodex namespace is not an exact local profile'
elif [[ "$skip_build" == true ]]; then
  fail '--skip-build requires an existing exact local Kodex profile'
fi

frontend_directory="$repository_root/services/staff/control-center"
[[ -f "$frontend_directory/package.json" ]] || fail 'Control Center package is absent'
if [[ "$check_only" == true ]]; then
  [[ -x "$frontend_directory/node_modules/.bin/tsc" &&
    -x "$frontend_directory/node_modules/.bin/playwright" ]] ||
    fail 'Control Center dependencies are absent; run npm ci in its directory'
fi
for target in "${targets[@]}"; do
  make --no-print-directory -n -C "$repository_root" "$target" >/dev/null ||
    fail "additional Make target is unavailable: $target"
done

if [[ "$check_only" == true ]]; then
  npm --prefix "$frontend_directory" run test:e2e:check
  printf 'Kodex full local E2E check completed for context %s\n' "$context"
  exit 0
fi

install -d -m 0700 "$state_directory" "$state_directory/e2e"
[[ "$(stat -c '%u' "$state_directory")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$state_directory") & 8#077)) == 0 ]] ||
  fail 'state directory must be owned by the current user and private'

summary_path="$state_directory/e2e/$resource_prefix-summary.json"
browser_report="$state_directory/e2e/$resource_prefix-report.json"
[[ ! -e "$summary_path" && ! -L "$summary_path" ]] || fail 'E2E summary already exists'
phases_file=$(mktemp)
printf '[]\n' >"$phases_file"
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)

append_phase() {
  local name=$1 status=$2 phase_started=$3 phase_finished=$4 temporary
  temporary=$(mktemp)
  jq --arg name "$name" --arg status "$status" --arg started_at "$phase_started" \
    --arg finished_at "$phase_finished" \
    '. + [{name:$name,status:$status,startedAt:$started_at,finishedAt:$finished_at}]' \
    "$phases_file" >"$temporary"
  mv -- "$temporary" "$phases_file"
}

run_phase() {
  local name=$1 phase_started phase_finished exit_code
  shift
  phase_started=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if "$@"; then
    phase_finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    append_phase "$name" passed "$phase_started" "$phase_finished"
    return 0
  else
    exit_code=$?
    phase_finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    append_phase "$name" failed "$phase_started" "$phase_finished"
    return "$exit_code"
  fi
}

write_summary() {
  local exit_code=$1 status=failed finished_at browser_summary targets_json batches_json temporary_summary
  [[ "$exit_code" -ne 0 ]] || status=passed
  finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  browser_summary=null
  if [[ -f "$browser_report" && ! -L "$browser_report" ]]; then
    browser_summary=$(jq -c '
      if .version == 1 and (.status | type == "string") and (.summary | type == "object")
      then {status:.status,counts:.summary}
      else null
      end
    ' "$browser_report" 2>/dev/null || printf 'null')
  fi
  if ((${#targets[@]} == 0)); then
    targets_json='[]'
  else
    targets_json=$(printf '%s\n' "${targets[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
  fi
  batches_json=$(
    for batch in "${canonical_batches[@]}"; do
      if batch_selected "$batch"; then
        printf '%s\n' "$batch"
      fi
    done | jq -Rsc 'split("\n") | map(select(length > 0))'
  )
  temporary_summary=$(mktemp "$summary_path.XXXXXX")
  jq -n \
    --arg status "$status" \
    --arg context "$context" \
    --arg resource_prefix "$resource_prefix" \
    --arg expected_sha "$expected_sha" \
    --arg started_at "$started_at" \
    --arg finished_at "$finished_at" \
    --arg build_mode "$([[ "$skip_build" == true ]] && printf reused || printf rebuilt)" \
    --argjson exit_code "$exit_code" \
    --argjson phases "$(<"$phases_file")" \
    --argjson browser "$browser_summary" \
    --argjson batches "$batches_json" \
    --argjson targets "$targets_json" '
      {
        version:1,
        status:$status,
        context:$context,
        resourcePrefix:$resource_prefix,
        expectedSHA:(if $expected_sha == "" then null else $expected_sha end),
        startedAt:$started_at,
        finishedAt:$finished_at,
        exitCode:$exit_code,
        buildMode:$build_mode,
        phases:$phases,
        browser:$browser,
        batches:$batches,
        additionalTargets:$targets
      }
    ' >"$temporary_summary"
  chmod 0600 "$temporary_summary"
  mv -- "$temporary_summary" "$summary_path"
  printf 'Redacted summary: %s\n' "$summary_path"
}

finalize() {
  local exit_code=$?
  trap - EXIT
  write_summary "$exit_code"
  rm -f -- "$phases_file"
  exit "$exit_code"
}
trap finalize EXIT

common_arguments=(
  --kubeconfig "$kubeconfig"
  --context "$context"
  --state-directory "$state_directory"
)
[[ -z "$cluster_marker" ]] || common_arguments+=(--cluster-marker "$cluster_marker")
[[ -z "$expected_sha" ]] || common_arguments+=(--expected-sha "$expected_sha")
if [[ "$skip_build" == true ]]; then
  run_phase local-readback "$repository_root/dev.sh" status "${common_arguments[@]}"
else
  run_phase local-render-deploy "$repository_root/dev.sh" up "${common_arguments[@]}"
fi
hot_reload_arguments=(
  --kubeconfig "$kubeconfig"
  --context "$context"
  --state-directory "$state_directory"
  --resource-prefix "$resource_prefix"
)
[[ -z "$expected_sha" ]] || hot_reload_arguments+=(--expected-sha "$expected_sha")
if batch_selected hot-reload; then
  run_phase go-and-vue-hot-reload-readback \
    "$repository_root/tools/dev/verify-hot-reload.sh" "${hot_reload_arguments[@]}"
fi
if batch_selected browser; then
  run_phase browser-auth-and-full-e2e "$repository_root/dev.sh" e2e \
    "${common_arguments[@]}" --resource-prefix "$resource_prefix" \
    --run-timeout-ms "$run_timeout_ms"
fi
run_deployed_integration_e2e() {
  local credentials_file="$state_directory/credentials.env" endpoint_ip dns_suffix public_host node_ca_file
  [[ -f "$credentials_file" && ! -L "$credentials_file" &&
    $((8#$(stat -c '%a' "$credentials_file") & 8#077)) == 0 ]] ||
    fail 'local owner credentials are absent or unsafe'
  # shellcheck disable=SC1090
  source "$credentials_file"
  endpoint_ip=${KODEX_DEV_ENDPOINT_IP:-127.0.0.1}
  dns_suffix=${endpoint_ip//./.}.nip.io
  public_host=${KODEX_DEV_PUBLIC_HOST:-control.$dns_suffix}
  node_ca_file=${NODE_EXTRA_CA_CERTS:-}
  if [[ "${KODEX_DEV_TLS_MODE:-local-ca}" == local-ca ]]; then
    node_ca_file="$state_directory/kodex-local-ca.crt"
    [[ -f "$node_ca_file" && ! -L "$node_ca_file" ]] ||
      fail 'local CA file is absent or unsafe'
  fi
  KODEX_E2E_BASE_URL="https://$public_host" \
    KODEX_E2E_OWNER_USERNAME="$KODEX_LOCAL_OWNER_USERNAME" \
    KODEX_E2E_OWNER_PASSWORD="$KODEX_LOCAL_OWNER_PASSWORD" \
    KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
    KODEX_E2E_RESOURCE_PREFIX="$resource_prefix" \
    KODEX_E2E_KUBECONFIG="$kubeconfig" \
    KODEX_E2E_KUBE_CONTEXT="$context" \
    KODEX_E2E_REPOSITORY_ROOT="$repository_root" \
    KODEX_E2E_STATE_DIRECTORY="$state_directory" \
    KODEX_E2E_BASE_HOST_RESOLUTION="${KODEX_E2E_BASE_HOST_RESOLUTION:-}" \
    NODE_EXTRA_CA_CERTS="$node_ca_file" \
    "$repository_root/scripts/tests/integration-deployed-e2e.sh"
}
if batch_selected integration; then
  run_phase deployed-integration-synthetic run_deployed_integration_e2e
fi
if batch_selected role-image; then
  run_phase role-image-build-admit-promote-runtime-readback env \
    KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
    "$repository_root/scripts/tests/local-role-image-supply-chain-e2e.sh" \
    "${common_arguments[@]}" --resource-prefix "$resource_prefix" \
    --timeout-seconds "$((run_timeout_ms / 1000))"
fi
if batch_selected archive; then
  run_phase session-archive-write-restore-delete-readback env \
    KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
    "$repository_root/scripts/tests/local-session-archive-e2e.sh" \
    "${common_arguments[@]}"
fi
if batch_selected backup; then
  run_phase backup-and-disposable-restore-drill env \
    KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
    "$repository_root/scripts/tests/local-backup-restore-e2e.sh" \
    "${common_arguments[@]}"
fi
for target in "${targets[@]}"; do
  run_phase "additional:$target" make --no-print-directory -C "$repository_root" "$target"
done

if [[ -n "$expected_sha" ]]; then
  [[ "$(git -C "$repository_root" rev-parse HEAD)" == "$expected_sha" &&
    -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] ||
    fail 'source checkout changed during acceptance E2E'
fi

printf 'Kodex full local E2E completed: %s\n' "$resource_prefix"
