#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf 'Integration synthetic fixture E2E failed: %s\n' "$*" >&2
  if [[ -f ${fixture_log:-} ]]; then
    printf '%s\n' '--- fixture log ---' >&2
    sed -n '1,120p' "$fixture_log" >&2
  fi
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
module_root="$repository_root/services/external/integration-gateway"
port=${KODEX_SYNTHETIC_FIXTURE_E2E_PORT:-0}

for command_name in curl go grep jq sed; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if [[ ! $port =~ ^[0-9]+$ ]] || ((port != 0 && port < 1024 || port > 65535)); then
  fail 'KODEX_SYNTHETIC_FIXTURE_E2E_PORT must be 0 or between 1024 and 65535'
fi

temporary_directory=$(mktemp -d)
fixture_log="$temporary_directory/fixture.log"
fixture_pid=''
cleanup() {
  if [[ -n $fixture_pid ]]; then
    kill "$fixture_pid" >/dev/null 2>&1 || true
    wait "$fixture_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

fixture_binary="$temporary_directory/integration-synthetic"
(
  cd -- "$module_root"
  env -u GOFLAGS GOENV=off GOWORK=off go build -trimpath -o "$fixture_binary" ./cmd/integration-synthetic
)
KODEX_INTEGRATION_SYNTHETIC_LISTEN_ADDRESS="127.0.0.1:$port" "$fixture_binary" >"$fixture_log" 2>&1 &
fixture_pid=$!
origin=''
if ((port != 0)); then
  origin="http://127.0.0.1:$port"
fi

ready=false
for _ in $(seq 1 80); do
  if [[ -z $origin ]]; then
    address=$(jq -Rr 'fromjson? | select(.msg == "integration synthetic fixture started") | .address // empty' "$fixture_log" | sed -n '$p')
    if [[ $address =~ ^127\.0\.0\.1:([0-9]+)$ ]] &&
      ((BASH_REMATCH[1] >= 1024 && BASH_REMATCH[1] <= 65535)); then
      origin="http://$address"
    fi
  fi
  if [[ -n $origin ]] && curl --silent --show-error --fail --max-time 1 "$origin/readyz" >/dev/null 2>&1; then
    ready=true
    break
  fi
  kill -0 "$fixture_pid" >/dev/null 2>&1 || fail 'fixture process stopped before readiness'
  sleep 0.1
done
[[ $ready == true ]] || fail 'fixture did not become ready'

response_body="$temporary_directory/response.json"
response_headers="$temporary_directory/response.headers"
request() {
  local method=$1
  local path=$2
  local body=${3:-}
  local effect_key=${4:-}
  local -a arguments=(
    --silent --show-error --max-time 5
    --request "$method"
    --dump-header "$response_headers"
    --output "$response_body"
    --write-out '%{http_code}'
    --header 'Accept: application/json'
  )
  if [[ -n $body ]]; then
    arguments+=(--header 'Content-Type: application/json' --data-binary "$body")
  fi
  if [[ -n $effect_key ]]; then
    arguments+=(--header "Idempotency-Key: $effect_key")
  fi
  curl "${arguments[@]}" "$origin$path"
}

assert_status() {
  local expected=$1
  local actual=$2
  local step=$3
  [[ $actual == "$expected" ]] || fail "$step returned HTTP $actual instead of $expected"
}

assert_json() {
  local expression=$1
  local step=$2
  jq -e "$expression" "$response_body" >/dev/null || fail "$step returned an invalid readback"
}

status=$(request GET /healthz)
assert_status 200 "$status" health
assert_json '.status == "ok"' health
status=$(request GET /readyz)
assert_status 200 "$status" readiness
assert_json '.status == "ready"' readiness

journal=fixture-crud
journal_path="/v1/journals/$journal"
mutation_path="$journal_path/entries"
diagnostic_path="/v1/diagnostics/journals/$journal"

status=$(request GET "$journal_path")
assert_status 200 "$status" initial-read
assert_json '.journal == "fixture-crud" and .sequence == 0 and .count == 0 and .value == ""' initial-read

create_body='{"action":"CREATE","value":"created"}'
status=$(request POST "$mutation_path" "$create_body" effect-create)
assert_status 200 "$status" create
assert_json '.journal == "fixture-crud" and .effect_key == "effect-create" and .sequence == 1 and .count == 1 and .value == "created"' create

status=$(request POST "$mutation_path" "$create_body" effect-create)
assert_status 200 "$status" create-replay
grep -Eiq '^Idempotency-Replayed:[[:space:]]*true' "$response_headers" || fail 'create replay header is absent'
assert_json '.sequence == 1 and .count == 1 and .value == "created"' create-replay

retry_body='{"action":"UPDATE","value":"updated","expected_sequence":1,"fault":"RETRYABLE_ONCE"}'
status=$(request POST "$mutation_path" "$retry_body" effect-update)
assert_status 503 "$status" retryable-first-attempt
grep -Eiq '^Retry-After:[[:space:]]*0' "$response_headers" || fail 'retryable error has no bounded Retry-After'
assert_json '.error == "synthetic_retryable_error"' retryable-first-attempt
status=$(request GET "$journal_path")
assert_status 200 "$status" retryable-non-mutation-readback
assert_json '.sequence == 1 and .count == 1 and .value == "created"' retryable-non-mutation-readback

status=$(request POST "$mutation_path" "$retry_body" effect-update)
assert_status 200 "$status" retryable-second-attempt
assert_json '.effect_key == "effect-update" and .sequence == 2 and .count == 1 and .value == "updated"' retryable-second-attempt

terminal_body='{"action":"DELETE","expected_sequence":2,"fault":"TERMINAL"}'
status=$(request POST "$mutation_path" "$terminal_body" effect-terminal)
assert_status 422 "$status" terminal-error
assert_json '.error == "synthetic_terminal_error"' terminal-error
status=$(request GET "$journal_path")
assert_status 200 "$status" terminal-non-mutation-readback
assert_json '.sequence == 2 and .count == 1 and .value == "updated"' terminal-non-mutation-readback

status=$(request POST "$mutation_path" '{"action":"DELETE","expected_sequence":1}' effect-stale)
assert_status 412 "$status" stale-delete
assert_json '.error == "version_conflict"' stale-delete

status=$(request POST "$mutation_path" '{"action":"DELETE","expected_sequence":2}' effect-delete)
assert_status 200 "$status" delete
assert_json '.effect_key == "effect-delete" and .sequence == 3 and .count == 0 and .value == "updated"' delete
status=$(request GET "$journal_path")
assert_status 200 "$status" deleted-readback
assert_json '.sequence == 3 and .count == 0 and .value == ""' deleted-readback
status=$(request GET "$diagnostic_path")
assert_status 200 "$status" diagnostic-readback
assert_json '
  .journal == "fixture-crud" and .exists == false and .sequence == 3 and .count == 0 and
  .last_effect_key == "effect-delete" and .replay_count == 1 and
  .last_replay_effect_key == "effect-create" and .retryable_failure_count == 1 and
  .terminal_failure_count == 1
' diagnostic-readback

printf 'Integration synthetic fixture E2E passed\n'
