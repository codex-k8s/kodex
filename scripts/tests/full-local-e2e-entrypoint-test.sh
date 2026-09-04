#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex full local E2E entrypoint test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
entrypoint="$repository_root/tools/dev/full-local-e2e.sh"
role_image_entrypoint="$repository_root/scripts/tests/local-role-image-supply-chain-e2e.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fixture_root="$temporary_directory/repository"
fake_bin="$temporary_directory/bin"
state_directory="$temporary_directory/state"
command_log="$temporary_directory/commands.log"
kubeconfig="$temporary_directory/kubeconfig"
mkdir -p "$fixture_root/tools/dev" "$fixture_root/scripts/tests" \
  "$fixture_root/services/staff/control-center" "$fake_bin"
cp "$entrypoint" "$fixture_root/tools/dev/full-local-e2e.sh"
cat >"$fixture_root/tools/dev/verify-hot-reload.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'hot-reload %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
EOF
printf '{}\n' >"$fixture_root/services/staff/control-center/package.json"
mkdir -p "$fixture_root/services/staff/control-center/node_modules/.bin"
printf '#!/usr/bin/env bash\nexit 0\n' \
  >"$fixture_root/services/staff/control-center/node_modules/.bin/tsc"
printf '#!/usr/bin/env bash\nexit 0\n' \
  >"$fixture_root/services/staff/control-center/node_modules/.bin/playwright"
chmod +x "$fixture_root/services/staff/control-center/node_modules/.bin/tsc" \
  "$fixture_root/services/staff/control-center/node_modules/.bin/playwright"
printf 'fixture\n' >"$kubeconfig"
install -d -m 0700 "$state_directory"
cat >"$state_directory/credentials.env" <<'EOF'
KODEX_LOCAL_OWNER_USERNAME=contract-owner
KODEX_LOCAL_OWNER_PASSWORD=contract-password
KODEX_DEV_TLS_MODE=public-acme
EOF
chmod 0600 "$state_directory/credentials.env"

cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'config current-context') printf '%s\n' "${KODEX_TEST_CONTEXT:?}" ;;
  'get --raw=/readyz') printf 'ok\n' ;;
  'get namespace/kodex-system -o json')
    [[ "${KODEX_TEST_NAMESPACE_PRESENT:-true}" == true ]] || exit 1
    printf '%s\n' '{"metadata":{"labels":{"app.kubernetes.io/part-of":"kodex","kodex.dev/environment":"staging","kodex.dev/local-profile":"hot-reload"}}}'
    ;;
  *) printf 'unexpected kubectl call: %s\n' "$*" >&2; exit 1 ;;
esac
EOF
cat >"$fake_bin/npm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'npm %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
EOF
cat >"$fake_bin/make" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'make %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
EOF
cat >"$fixture_root/dev.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'dev %s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
command_name=${1:?}
shift
state_directory=""
resource_prefix=""
while (($# > 0)); do
  case "$1" in
    --state-directory) state_directory=${2:?}; shift 2 ;;
    --resource-prefix) resource_prefix=${2:?}; shift 2 ;;
    *) shift 2 ;;
  esac
done
if [[ "$command_name" == e2e ]]; then
  mkdir -p "$state_directory/e2e"
  jq -n '{version:1,status:"passed",summary:{passed:7},results:[]}' \
    >"$state_directory/e2e/$resource_prefix-report.json"
fi
EOF
for local_e2e in integration-deployed-e2e.sh local-role-image-supply-chain-e2e.sh \
  local-session-archive-e2e.sh local-backup-restore-e2e.sh; do
  cat >"$fixture_root/scripts/tests/$local_e2e" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'storage-e2e %s %s\n' "$(basename "$0")" "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
EOF
done
chmod +x "$fake_bin"/* "$fixture_root/dev.sh" \
  "$fixture_root/tools/dev/full-local-e2e.sh" "$fixture_root/tools/dev/verify-hot-reload.sh" \
  "$fixture_root/scripts/tests"/*.sh
git -C "$fixture_root" init -q
git -C "$fixture_root" add .
git -C "$fixture_root" -c user.name=fixture -c user.email=fixture@kodex.local \
  commit -qm fixture
fixture_sha=$(git -C "$fixture_root" rev-parse HEAD)

export PATH="$fake_bin:$PATH"
export KODEX_TEST_COMMAND_LOG="$command_log"
export KODEX_TEST_CONTEXT=fixture-local

for public_role_image_contract in \
  'tls_mode=${KODEX_DEV_TLS_MODE:-local-ca}' \
  'public-acme)' \
  'public_host=${KODEX_DEV_PUBLIC_HOST:-}' \
  'base_url="https://$public_host/"' \
  'ca_file=""'; do
  rg -Fq -- "$public_role_image_contract" "$role_image_entrypoint" ||
    fail "public RoleImage E2E contract is absent: $public_role_image_contract"
done

"$fixture_root/tools/dev/full-local-e2e.sh" --check \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-check \
  --target test-extra >/dev/null
grep -Fxq 'npm --prefix '"$fixture_root"'/services/staff/control-center run test:e2e:check' \
  "$command_log" || fail '--check did not validate the browser suite'
grep -Fxq 'make --no-print-directory -n -C '"$fixture_root"' test-extra' "$command_log" ||
  fail '--check did not validate the additional target'
if grep -q '^dev ' "$command_log"; then
  fail '--check invoked mutating local development commands'
fi

: >"$command_log"
KODEX_TEST_CREDENTIAL='must-not-be-persisted' \
  "$fixture_root/tools/dev/full-local-e2e.sh" \
    --kubeconfig "$kubeconfig" --context fixture-local \
    --state-directory "$state_directory" --resource-prefix contract-full \
    --run-timeout-ms 60000 --target test-extra >/dev/null
grep -Fq 'dev up ' "$command_log" || fail 'full run did not delegate deployment to dev.sh up'
grep -Fq 'dev e2e ' "$command_log" || fail 'full run did not delegate browser E2E to dev.sh e2e'
grep -Fq 'hot-reload --kubeconfig ' "$command_log" ||
  fail 'full run did not verify Go and Vue hot reload'
grep -Fq 'storage-e2e integration-deployed-e2e.sh ' "$command_log" ||
  fail 'full run did not execute deployed integration E2E'
grep -Fq 'storage-e2e local-role-image-supply-chain-e2e.sh ' "$command_log" ||
  fail 'full run did not execute role image supply chain readback'
grep -Fq 'storage-e2e local-session-archive-e2e.sh ' "$command_log" ||
  fail 'full run did not execute session archive readback'
grep -Fq 'storage-e2e local-backup-restore-e2e.sh ' "$command_log" ||
  fail 'full run did not execute disposable backup restore drill'
grep -Fxq 'make --no-print-directory -C '"$fixture_root"' test-extra' "$command_log" ||
  fail 'full run did not execute the additional target'
summary="$state_directory/e2e/contract-full-summary.json"
jq -e '
  .version == 1 and .status == "passed" and .context == "fixture-local" and
  .resourcePrefix == "contract-full" and .buildMode == "rebuilt" and
  .browser == {status:"passed",counts:{passed:7}} and
  .batches == ["hot-reload","browser","integration","role-image","archive","backup"] and
  .additionalTargets == ["test-extra"] and
  [.phases[].name] == ["local-render-deploy","go-and-vue-hot-reload-readback",
    "browser-auth-and-full-e2e","deployed-integration-synthetic",
    "role-image-build-admit-promote-runtime-readback",
    "session-archive-write-restore-delete-readback","backup-and-disposable-restore-drill",
    "additional:test-extra"] and
  all(.phases[]; .status == "passed")
' "$summary" >/dev/null || fail 'redacted full-run summary is invalid'
[[ "$(stat -c '%a' "$summary")" == 600 ]] || fail 'summary permissions are not private'
if grep -Fq 'must-not-be-persisted' "$summary"; then
  fail 'summary persisted a credential value'
fi

: >"$command_log"
"$fixture_root/tools/dev/full-local-e2e.sh" --skip-build \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-reuse \
  --run-timeout-ms 60000 >/dev/null
grep -Fq 'dev status ' "$command_log" || fail '--skip-build did not perform readback'
grep -Fq 'dev e2e ' "$command_log" || fail '--skip-build did not run browser E2E'
if grep -Fq 'dev up ' "$command_log"; then
  fail '--skip-build invoked build or deployment'
fi
jq -e '.status == "passed" and .buildMode == "reused"' \
  "$state_directory/e2e/contract-reuse-summary.json" >/dev/null ||
  fail '--skip-build summary is invalid'

: >"$command_log"
"$fixture_root/tools/dev/full-local-e2e.sh" --skip-build \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-batches \
  --run-timeout-ms 60000 --batch backup --batch integration >/dev/null
grep -Fq 'dev status ' "$command_log" || fail 'selected batches did not perform readback'
grep -Fq 'storage-e2e integration-deployed-e2e.sh ' "$command_log" ||
  fail 'selected integration batch was not executed'
grep -Fq 'storage-e2e local-backup-restore-e2e.sh ' "$command_log" ||
  fail 'selected backup batch was not executed'
if grep -Eq '^(dev e2e|hot-reload|storage-e2e local-role-image-supply-chain-e2e.sh|storage-e2e local-session-archive-e2e.sh)' "$command_log"; then
  fail 'unselected E2E batch was executed'
fi
jq -e '
  .status == "passed" and .buildMode == "reused" and
  .batches == ["integration","backup"] and
  [.phases[].name] == ["local-readback","deployed-integration-synthetic",
    "backup-and-disposable-restore-drill"]
' "$state_directory/e2e/contract-batches-summary.json" >/dev/null ||
  fail 'selected batch summary or canonical order is invalid'

: >"$command_log"
"$fixture_root/tools/dev/full-local-e2e.sh" --skip-build \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-arguments \
  --cluster-marker /var/lib/kodex-dev/cluster-identity.json \
  --expected-sha "$fixture_sha" --run-timeout-ms 60000 --batch role-image >/dev/null
grep -Fq 'dev status --kubeconfig '"$kubeconfig"' --context fixture-local --state-directory '"$state_directory"' --cluster-marker /var/lib/kodex-dev/cluster-identity.json --expected-sha '"$fixture_sha" \
  "$command_log" || fail 'deployment readback did not receive attestation arguments'
role_image_command=$(grep -F 'storage-e2e local-role-image-supply-chain-e2e.sh ' "$command_log")
[[ "$role_image_command" != *'--cluster-marker'* && "$role_image_command" != *'--expected-sha'* ]] ||
  fail 'RoleImage E2E received unsupported deployment arguments'

if "$fixture_root/tools/dev/full-local-e2e.sh" --check \
  --kubeconfig "$kubeconfig" --context production-cluster \
  --state-directory "$state_directory" --resource-prefix contract-prod >/dev/null 2>&1; then
  fail 'production context was accepted'
fi
if "$fixture_root/tools/dev/full-local-e2e.sh" --check \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-target \
  --target deploy-production >/dev/null 2>&1; then
  fail 'unsafe additional target was accepted'
fi
if "$fixture_root/tools/dev/full-local-e2e.sh" --check \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-batch \
  --batch unknown >/dev/null 2>&1; then
  fail 'unknown E2E batch was accepted'
fi
if "$fixture_root/tools/dev/full-local-e2e.sh" --check \
  --kubeconfig "$kubeconfig" --context fixture-local \
  --state-directory "$state_directory" --resource-prefix contract-batch \
  --batch browser --batch browser >/dev/null 2>&1; then
  fail 'duplicated E2E batch was accepted'
fi

printf 'Kodex full local E2E entrypoint tests passed\n'
