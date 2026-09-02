#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Deployed integration E2E contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
integration_spec="$repository_root/services/staff/control-center/e2e/integration-path.spec.ts"
grep -Fq 'test("опциональный GitHub READ и обратимый WRITE проходят через MCP"' \
  "$integration_spec" || fail 'GitHub integration test title changed without runner update'
grep -Fq 'test("опциональный API key account выполняет run с exact affinity"' \
  "$integration_spec" || fail 'API key integration test title changed without runner update'
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fixture_root="$temporary_directory/repository"
fake_bin="$temporary_directory/bin"
state_directory="$temporary_directory/state"
command_log="$temporary_directory/commands.log"
kubeconfig="$temporary_directory/kubeconfig"
mkdir -p -- "$fixture_root/scripts/tests" \
  "$fixture_root/services/staff/control-center/node_modules/.bin" "$fake_bin" "$state_directory"
chmod 0700 -- "$state_directory"
cp -- "$repository_root/scripts/tests/integration-deployed-e2e.sh" "$fixture_root/scripts/tests/"
printf 'fixture\n' >"$kubeconfig"

cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
if [[ "$*" == *' port-forward '* ]]; then
  printf 'Forwarding from 127.0.0.1:18082 -> 8080\n'
  exec sleep 300
fi
exit 0
EOF
cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
cat >"$fake_bin/npm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'npm %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
[[ "${KODEX_TEST_FAIL_PROFILE:-}" != auth ]]
EOF
cat >"$fixture_root/services/staff/control-center/node_modules/.bin/playwright" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'playwright %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
case "$*" in
  *'--grep-invert'*) [[ "${KODEX_TEST_FAIL_PROFILE:-}" != core ]] ;;
  *'опциональный GitHub'*) [[ "${KODEX_TEST_FAIL_PROFILE:-}" != github ]] ;;
  *'опциональный API key'*) [[ "${KODEX_TEST_FAIL_PROFILE:-}" != provider-api-key ]] ;;
  *) [[ "${KODEX_TEST_FAIL_PROFILE:-}" != core ]] ;;
esac
EOF
chmod +x -- "$fake_bin"/* \
  "$fixture_root/scripts/tests/integration-deployed-e2e.sh" \
  "$fixture_root/services/staff/control-center/node_modules/.bin/playwright"

export PATH="$fake_bin:$PATH"
export KODEX_TEST_COMMAND_LOG="$command_log"
common_environment=(
  KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION
  KODEX_E2E_BASE_URL=https://control.example.test
  KODEX_E2E_OWNER_USERNAME=contract-owner
  KODEX_E2E_OWNER_PASSWORD=contract-password
  KODEX_E2E_KUBECONFIG="$kubeconfig"
  KODEX_E2E_STATE_DIRECTORY="$state_directory"
)

: >"$command_log"
env "${common_environment[@]}" KODEX_E2E_RESOURCE_PREFIX=integration-disabled \
  "$fixture_root/scripts/tests/integration-deployed-e2e.sh" >/dev/null
disabled_status="$state_directory/e2e/integration-disabled-integration-status.json"
jq -e '
  .status == "PASS" and .profiles.core.status == "PASS" and
  .profiles.github == {requested:false,status:"NOT RUN"} and
  .profiles.providerApiKey == {requested:false,status:"NOT RUN"}
' "$disabled_status" >/dev/null || fail 'disabled optional profile status is invalid'
[[ "$(grep -c '^playwright ' "$command_log")" == 1 ]] ||
  fail 'disabled optional profiles were executed'

: >"$command_log"
env "${common_environment[@]}" \
  KODEX_E2E_RESOURCE_PREFIX=integration-enabled \
  KODEX_INTEGRATION_E2E_GITHUB=1 \
  KODEX_GITHUB_BOT_PAT=contract-github-token \
  KODEX_PROVIDER_E2E_API_KEY=1 \
  OPENAI_API_KEY=contract-provider-key \
  "$fixture_root/scripts/tests/integration-deployed-e2e.sh" >/dev/null
enabled_status="$state_directory/e2e/integration-enabled-integration-status.json"
jq -e '
  .status == "PASS" and
  .profiles.core == {requested:true,status:"PASS"} and
  .profiles.github == {requested:true,status:"PASS"} and
  .profiles.providerApiKey == {requested:true,status:"PASS"}
' "$enabled_status" >/dev/null || fail 'enabled integration profile status is invalid'
[[ "$(grep -c '^playwright ' "$command_log")" == 3 ]] ||
  fail 'enabled integration profiles were not executed independently'

: >"$command_log"
if env "${common_environment[@]}" \
  KODEX_E2E_RESOURCE_PREFIX=integration-failed \
  KODEX_INTEGRATION_E2E_GITHUB=1 \
  KODEX_GITHUB_BOT_PAT=contract-github-token \
  KODEX_PROVIDER_E2E_API_KEY=1 \
  OPENAI_API_KEY=contract-provider-key \
  KODEX_TEST_FAIL_PROFILE=github \
  "$fixture_root/scripts/tests/integration-deployed-e2e.sh" >/dev/null 2>&1; then
  fail 'failed GitHub integration profile was accepted'
fi
failed_status="$state_directory/e2e/integration-failed-integration-status.json"
jq -e '
  .status == "FAIL" and .profiles.core.status == "PASS" and
  .profiles.github.status == "FAIL" and .profiles.providerApiKey.status == "PASS"
' "$failed_status" >/dev/null || fail 'failed integration profile was not isolated'

if grep -RFn 'contract-github-token' "$state_directory" >/dev/null ||
  grep -RFn 'contract-provider-key' "$state_directory" >/dev/null; then
  fail 'integration status persisted a credential value'
fi
[[ "$(stat -c '%a' "$enabled_status")" == 600 ]] ||
  fail 'integration status is not owner-private'

printf 'Deployed integration E2E contract tests passed\n'
