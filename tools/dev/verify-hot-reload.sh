#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex hot reload verification failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --kubeconfig <path> --context <exact-context>" \
    '  --state-directory <private-path> --resource-prefix <slug>' \
    '  [--expected-sha <40-hex-commit>]' >&2
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
kubeconfig=""
context=""
state_directory=""
expected_sha=""
resource_prefix=""
while (($# > 0)); do
  case "$1" in
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:-}; shift 2 ;;
    --expected-sha) expected_sha=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

for command_name in cp curl date git grep install jq kubectl mktemp python3 seq stat touch; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is absent or unsafe'
[[ -n "$context" && "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'exact non-production Kubernetes context is required'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" &&
  -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'private state directory is absent or unsafe'
[[ "$(stat -c '%u' "$state_directory")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$state_directory") & 8#077)) == 0 ]] ||
  fail 'state directory must be owned by the current user and private'
[[ "$resource_prefix" =~ ^[a-z0-9]([a-z0-9-]{2,38}[a-z0-9])$ ]] ||
  fail 'resource prefix must be a lowercase 4-40 character slug'

source_sha=$(git -C "$repository_root" rev-parse HEAD)
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'source SHA is invalid'
if [[ -n "$expected_sha" ]]; then
  [[ "$expected_sha" =~ ^[a-f0-9]{40}$ && "$source_sha" == "$expected_sha" ]] ||
    fail 'source HEAD does not match the expected SHA'
fi
[[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] ||
  fail 'hot reload verification requires a clean source checkout'

export KUBECONFIG="$kubeconfig"
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
kubectl get namespace/kodex-system -o json | jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/environment"] == "staging" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
' >/dev/null || fail 'Kubernetes namespace is not an exact disposable hot reload profile'
kubectl -n kodex-system get deployment/control-api-gateway deployment/staff-control-center >/dev/null ||
  fail 'hot reload workloads are absent'

go_source="$repository_root/services/external/control-api-gateway/internal/app/app.go"
vue_css_source="$repository_root/services/staff/control-center/src/app/styles/base.css"
for source_file in "$go_source" "$vue_css_source"; do
  [[ -f "$source_file" && ! -L "$source_file" ]] || fail 'hot reload source is absent or unsafe'
done

evidence_directory="$state_directory/e2e"
install -d -m 0700 "$evidence_directory"
evidence_file="$evidence_directory/$resource_prefix-hot-reload.json"
[[ ! -e "$evidence_file" && ! -L "$evidence_file" ]] ||
  fail 'hot reload evidence already exists for this resource prefix'
backup_directory=$(mktemp -d "$state_directory/.hot-reload.XXXXXX")
cp -p -- "$go_source" "$backup_directory/app.go"
cp -p -- "$vue_css_source" "$backup_directory/base.css"

go_port=""
vue_port=""
go_forward_pid=""
vue_forward_pid=""
go_forward_log="$backup_directory/go-port-forward.log"
vue_forward_log="$backup_directory/vue-port-forward.log"
source_modified=false
cleanup() {
  local exit_code=$?
  set +e
  if [[ "$source_modified" == true ]]; then
    cp -p -- "$backup_directory/app.go" "$go_source"
    cp -p -- "$backup_directory/base.css" "$vue_css_source"
    touch -- "$go_source" "$vue_css_source"
  fi
  [[ -z "$go_forward_pid" ]] || kill "$go_forward_pid" 2>/dev/null
  [[ -z "$vue_forward_pid" ]] || kill "$vue_forward_pid" 2>/dev/null
  [[ -z "$go_forward_pid" ]] || wait "$go_forward_pid" 2>/dev/null
  [[ -z "$vue_forward_pid" ]] || wait "$vue_forward_pid" 2>/dev/null
  rm -rf -- "$backup_directory"
  exit "$exit_code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

reserve_port() {
  python3 - <<'PY'
import socket

with socket.socket() as listener:
    listener.bind(("127.0.0.1", 0))
    print(listener.getsockname()[1])
PY
}

start_port_forward() {
  local service=$1 remote_port=$2 local_port=$3 log_file=$4 pid_variable=$5 pid previous_pid
  previous_pid=${!pid_variable:-}
  if [[ -n "$previous_pid" ]]; then
    kill "$previous_pid" 2>/dev/null || true
    wait "$previous_pid" 2>/dev/null || true
  fi
  kubectl -n kodex-system port-forward --address 127.0.0.1 \
    "service/$service" "$local_port:$remote_port" >"$log_file" 2>&1 &
  pid=$!
  printf -v "$pid_variable" '%s' "$pid"
  for _ in $(seq 1 100); do
    grep -Fq "Forwarding from 127.0.0.1:$local_port" "$log_file" && return 0
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  fail "port-forward did not start for $service"
}

ensure_port_forward() {
  local service=$1 remote_port=$2 local_port=$3 log_file=$4 pid_variable=$5 current_pid
  current_pid=${!pid_variable:-}
  if [[ -n "$current_pid" ]] && kill -0 "$current_pid" 2>/dev/null; then
    return 0
  fi
  start_port_forward "$service" "$remote_port" "$local_port" "$log_file" "$pid_variable"
}

wait_for_status() {
  local expected=$1 last_status=unreachable
  for _ in $(seq 1 300); do
    ensure_port_forward control-api-gateway 9090 "$go_port" \
      "$go_forward_log" go_forward_pid
    last_status=$(curl --silent --output /dev/null --write-out '%{http_code}' \
      --max-time 2 "http://127.0.0.1:$go_port/healthz" || true)
    [[ "$last_status" == "$expected" ]] && return 0
    sleep 1
  done
  fail "Go hot reload did not expose HTTP $expected; last observed status: $last_status"
}

read_vue_css() {
  local public_host=$1 cache_buster=$2
  curl --silent --show-error --fail --max-time 5 \
    --header "Host: $public_host" \
    "http://127.0.0.1:$vue_port/src/app/styles/base.css?proof=$cache_buster"
}

wait_for_vue_marker() {
  local public_host=$1 marker=$2 present=$3 module
  for _ in $(seq 1 180); do
    ensure_port_forward staff-control-center 8080 "$vue_port" \
      "$vue_forward_log" vue_forward_pid
    module=$(read_vue_css "$public_host" "$RANDOM" 2>/dev/null || true)
    if [[ "$present" == true && "$module" == *"$marker"* ]]; then
      return 0
    fi
    if [[ "$present" == false && "$module" != *"$marker"* && -n "$module" ]]; then
      return 0
    fi
    sleep 1
  done
  fail 'Vue hot reload marker did not reach the expected state'
}

go_port=$(reserve_port)
while [[ -z "$vue_port" || "$vue_port" == "$go_port" ]]; do
  vue_port=$(reserve_port)
done
start_port_forward control-api-gateway 9090 "$go_port" \
  "$go_forward_log" go_forward_pid
start_port_forward staff-control-center 8080 "$vue_port" \
  "$vue_forward_log" vue_forward_pid
public_host=$(kubectl -n kodex-system get ingress/staff-control-center \
  -o jsonpath='{.spec.rules[0].host}')
[[ "$public_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$public_host" == *.* ]] ||
  fail 'Control Center public host is invalid'

started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
wait_for_status 204
marker=KODEX_HOT_RELOAD_E2E_MARKER
source_modified=true
python3 - "$go_source" "$vue_css_source" "$marker" <<'PY' || fail 'source marker injection failed'
from pathlib import Path
import sys

go_path = Path(sys.argv[1])
css_path = Path(sys.argv[2])
marker = sys.argv[3]
go_text = go_path.read_text(encoding="utf-8")
go_old = 'technicalMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })'
go_new = go_old.replace("http.StatusNoContent", "http.StatusAccepted")
if go_text.count(go_old) != 1:
    raise SystemExit(1)
go_path.write_text(go_text.replace(go_old, go_new), encoding="utf-8")

css_text = css_path.read_text(encoding="utf-8")
if marker in css_text:
    raise SystemExit(1)
css_path.write_text(f"{css_text.rstrip()}\n\n/* {marker} */\n", encoding="utf-8")
PY
wait_for_status 202
wait_for_vue_marker "$public_host" "$marker" true

cp -p -- "$backup_directory/app.go" "$go_source"
cp -p -- "$backup_directory/base.css" "$vue_css_source"
touch -- "$go_source" "$vue_css_source"
wait_for_status 204
wait_for_vue_marker "$public_host" "$marker" false
source_modified=false
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$source_sha" &&
  -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] ||
  fail 'source checkout was not restored after hot reload verification'

finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
temporary_evidence=$(mktemp "$evidence_directory/.hot-reload.XXXXXX")
jq -n --arg source_sha "$source_sha" --arg resource_prefix "$resource_prefix" \
  --arg started_at "$started_at" \
  --arg finished_at "$finished_at" '
  {
    version: 1,
    status: "passed",
    sourceSHA: $source_sha,
    resourcePrefix: $resource_prefix,
    startedAt: $started_at,
    finishedAt: $finished_at,
    go: {baselineHTTPStatus: 204, changedHTTPStatus: 202, restoredHTTPStatus: 204},
    vue: {markerObserved: true, markerRemovedAfterRestore: true},
    sourceRestored: true
  }
' >"$temporary_evidence"
chmod 0600 "$temporary_evidence"
mv -- "$temporary_evidence" "$evidence_file"
printf 'Kodex Go and Vue hot reload verification passed for source SHA %s\n' "$source_sha"
