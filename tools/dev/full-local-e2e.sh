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
    '  [--resource-prefix <slug>] [--run-timeout-ms <milliseconds>]' \
    '  [--target <test-make-target>]...' >&2
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
kubeconfig=${KODEX_DEV_KUBECONFIG:-/home/s/.kube/kodex-dev-local}
context=""
state_directory="$repository_root/.kodex-dev"
resource_prefix="full-local-e2e-$(date -u +%Y%m%d%H%M%S)"
run_timeout_ms=900000
check_only=false
skip_build=false
targets=()

while (($# > 0)); do
  case "$1" in
    --check) check_only=true; shift ;;
    --skip-build) skip_build=true; shift ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --run-timeout-ms) run_timeout_ms=${2:-}; shift 2 ;;
    --target) targets+=("${2:-}"); shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

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
  local exit_code=$1 status=failed finished_at browser_summary targets_json temporary_summary
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
  temporary_summary=$(mktemp "$summary_path.XXXXXX")
  jq -n \
    --arg status "$status" \
    --arg context "$context" \
    --arg resource_prefix "$resource_prefix" \
    --arg started_at "$started_at" \
    --arg finished_at "$finished_at" \
    --arg build_mode "$([[ "$skip_build" == true ]] && printf reused || printf rebuilt)" \
    --argjson exit_code "$exit_code" \
    --argjson phases "$(<"$phases_file")" \
    --argjson browser "$browser_summary" \
    --argjson targets "$targets_json" '
      {
        version:1,
        status:$status,
        context:$context,
        resourcePrefix:$resource_prefix,
        startedAt:$started_at,
        finishedAt:$finished_at,
        exitCode:$exit_code,
        buildMode:$build_mode,
        phases:$phases,
        browser:$browser,
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
if [[ "$skip_build" == true ]]; then
  run_phase local-readback "$repository_root/dev.sh" status "${common_arguments[@]}"
else
  run_phase local-render-deploy "$repository_root/dev.sh" up "${common_arguments[@]}"
fi
run_phase browser-auth-and-full-e2e "$repository_root/dev.sh" e2e \
  "${common_arguments[@]}" --resource-prefix "$resource_prefix" \
  --run-timeout-ms "$run_timeout_ms"
run_phase session-archive-write-restore-delete-readback env \
  KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
  "$repository_root/scripts/tests/local-session-archive-e2e.sh" \
  "${common_arguments[@]}"
run_phase backup-and-disposable-restore-drill env \
  KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
  "$repository_root/scripts/tests/local-backup-restore-e2e.sh" \
  "${common_arguments[@]}"
for target in "${targets[@]}"; do
  run_phase "additional:$target" make --no-print-directory -C "$repository_root" "$target"
done

printf 'Kodex full local E2E completed: %s\n' "$resource_prefix"
