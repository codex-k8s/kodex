#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local OIDC E2E group fixture contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
fixture_script="$repository_root/tools/dev/prepare-e2e-oidc-group.sh"
dev_script="$repository_root/dev.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fake_bin="$temporary_directory/bin"
state_directory="$temporary_directory/state"
command_log="$temporary_directory/external-commands.log"
mkdir -p "$fake_bin" "$state_directory"

for command_name in base64 flock jq kubectl; do
  cat >"$fake_bin/$command_name" <<'EOF'
#!/usr/bin/env bash
printf '%s %s\n' "${0##*/}" "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
exit 97
EOF
  chmod +x "$fake_bin/$command_name"
done

expect_guard_failure() {
  local name=$1 expected_message=$2
  shift 2
  local output_file="$temporary_directory/$name.output"
  : >"$command_log"
  if env PATH="$fake_bin:$PATH" KODEX_TEST_COMMAND_LOG="$command_log" \
    "$@" >"$output_file" 2>&1; then
    fail "$name guard accepted an unsafe invocation"
  fi
  grep -Fq "$expected_message" "$output_file" ||
    fail "$name guard did not report the expected failure"
  [[ ! -s "$command_log" ]] ||
    fail "$name guard invoked an external cluster or secret command"
}

expect_guard_failure state-directory \
  'state directory must be an exact existing safe absolute path' \
  env KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
  bash "$fixture_script" --context local-e2e --state-directory relative/state

expect_guard_failure disposable-confirmation \
  'disposable installation confirmation is required' \
  env -u KODEX_E2E_CONFIRM_DISPOSABLE \
  bash "$fixture_script" --context local-e2e --state-directory "$state_directory"

expect_guard_failure production-context \
  'production context is forbidden' \
  env KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION \
  bash "$fixture_script" --context production-cluster --state-directory "$state_directory"

fixture_reference='"$repository_root/tools/dev/prepare-e2e-oidc-group.sh"'
[[ $(grep -Fc "$fixture_reference" "$dev_script") -eq 1 ]] ||
  fail 'dev.sh must invoke the OIDC fixture exactly once'
fixture_line=$(grep -Fn "$fixture_reference" "$dev_script" | cut -d: -f1)
[[ "$fixture_line" =~ ^[0-9]+$ && "$fixture_line" -ge 3 ]] ||
  fail 'OIDC fixture invocation line is unavailable'
fixture_block=$(sed -n "$((fixture_line - 2)),$((fixture_line + 1))p" "$dev_script")
grep -Fq 'if [[ "$command_name" == e2e ]]; then' <<<"$fixture_block" ||
  fail 'OIDC fixture invocation is not guarded by the e2e command'
grep -Fq 'KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION' \
  <<<"$fixture_block" || fail 'dev.sh does not confirm the disposable fixture explicitly'
grep -Fq -- '--state-directory "$state_directory"' <<<"$fixture_block" ||
  fail 'dev.sh does not bind the fixture to the selected state directory'

assert_rbac_group_before_command() {
  local command_pattern=$1 label=$2 lookback=$3
  local command_line
  command_line=$(grep -Fn "$command_pattern" "$dev_script" | cut -d: -f1)
  [[ "$command_line" =~ ^[0-9]+$ ]] || fail "$label command is unavailable"
  local first_line=$((command_line - lookback))
  ((first_line > 0)) || first_line=1
  sed -n "${first_line},${command_line}p" "$dev_script" |
    grep -Fq 'KODEX_E2E_RBAC_GROUP=kodex-e2e-restricted' ||
    fail "$label does not receive KODEX_E2E_RBAC_GROUP"
}

[[ $(grep -Fc 'KODEX_E2E_RBAC_GROUP=kodex-e2e-restricted' "$dev_script") -eq 2 ]] ||
  fail 'RBAC group must be passed only to local smoke and discovery E2E'
assert_rbac_group_before_command 'run test:e2e:local' 'local browser smoke' 12
assert_rbac_group_before_command 'run test:e2e:discovery' 'browser discovery E2E' 14

printf 'Kodex local OIDC E2E group fixture contract test passed\n'
