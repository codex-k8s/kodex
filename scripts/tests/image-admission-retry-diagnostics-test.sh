#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Image admission retry diagnostics test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
admission_script="$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

extract_function() {
  local function_name=$1
  awk -v header="$function_name() {" '
    $0 == header { found = 1 }
    found { print }
    found && $0 == "}" { exit }
  ' "$admission_script"
}

for constant in CLAIM_RETRY_ATTEMPT_LIMIT CLAIM_RETRY_DELAY_SECONDS \
  CLAIM_RETRY_DIAGNOSTIC_INTERVAL CLAIM_RETRY_DIAGNOSTIC_MAX_BYTES \
  CLAIM_RETRY_DIAGNOSTIC_PREFIX CLAIM_RETRY_STORAGE_FAILURE; do
  declaration=$(grep -E "^${constant}=" "$admission_script")
  [[ -n $declaration ]] || fail "missing retry constant: $constant"
  eval "$declaration"
done
[[ $CLAIM_RETRY_ATTEMPT_LIMIT == 120 ]] || fail 'production attempt limit changed'
[[ $CLAIM_RETRY_DELAY_SECONDS == 5 ]] || fail 'production retry delay changed'
[[ $CLAIM_RETRY_DIAGNOSTIC_INTERVAL == 12 ]] || fail 'production diagnostic interval is not one minute'
[[ $CLAIM_RETRY_DIAGNOSTIC_MAX_BYTES == 160 ]] || fail 'diagnostic size bound changed'

for function_name in classify_claim_retry_error emit_claim_retry_diagnostic \
  claim_owner_work claim_promotion claim_admission; do
  definition=$(extract_function "$function_name")
  [[ -n $definition ]] || fail "missing function: $function_name"
  eval "$definition"
done

fake_bin="$temporary_directory/bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/image-admission-bridge" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
count=$(<"${KODEX_TEST_BRIDGE_COUNT_FILE:?}")
count=$((count + 1))
printf '%s\n' "$count" >"$KODEX_TEST_BRIDGE_COUNT_FILE"
stat -Lc '%a' /proc/self/fd/2 >>"${KODEX_TEST_STDERR_MODE_FILE:?}"
if (( count > KODEX_TEST_BRIDGE_FAILURES )); then
  exit 0
fi
case "$count" in
  1) printf 'rpc error: code = Unavailable desc = token=raw-secret-must-not-leak\n' >&2 ;;
  6) printf 'rpc error: code = PermissionDenied desc = token=raw-secret-must-not-leak\n' >&2 ;;
  12) printf 'x509: certificate signed by unknown authority; token=raw-secret-must-not-leak\n' >&2 ;;
  *) printf 'required image owner environment is invalid: raw-secret-must-not-leak\n' >&2 ;;
esac
exit 1
EOF
cat >"$fake_bin/sleep" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "${1:-}" >>"${KODEX_TEST_SLEEP_LOG:?}"
EOF
chmod +x "$fake_bin/image-admission-bridge" "$fake_bin/sleep"
export PATH="$fake_bin:$PATH"

bridge_count_file="$temporary_directory/bridge-count"
stderr_mode_file="$temporary_directory/stderr-mode"
sleep_log="$temporary_directory/sleep-log"
export KODEX_TEST_BRIDGE_COUNT_FILE="$bridge_count_file"
export KODEX_TEST_STDERR_MODE_FILE="$stderr_mode_file"
export KODEX_TEST_SLEEP_LOG="$sleep_log"

CLAIM_RETRY_ATTEMPT_LIMIT=14
CLAIM_RETRY_DELAY_SECONDS=0
CLAIM_RETRY_DIAGNOSTIC_INTERVAL=6

printf '0\n' >"$bridge_count_file"
: >"$stderr_mode_file"
: >"$sleep_log"
export KODEX_TEST_BRIDGE_FAILURES=13
claim_admission 2>"$temporary_directory/admission.stderr" || fail 'admission did not recover'
[[ $(<"$bridge_count_file") == 14 ]] || fail 'admission retry count drifted'
[[ $(wc -l <"$temporary_directory/admission.stderr") == 3 ]] || fail 'admission diagnostic rate limit drifted'
grep -Fq 'operation=claim attempt=1/14 class=dependency-unavailable' "$temporary_directory/admission.stderr" ||
  fail 'first admission error was not classified immediately'
grep -Fq 'operation=claim attempt=6/14 class=authorization' "$temporary_directory/admission.stderr" ||
  fail 'latest authorization error was not classified'
grep -Fq 'operation=claim attempt=12/14 class=transport-trust' "$temporary_directory/admission.stderr" ||
  fail 'latest TLS error was not classified'

printf '0\n' >"$bridge_count_file"
: >"$stderr_mode_file"
: >"$sleep_log"
export KODEX_TEST_BRIDGE_FAILURES=14
if (claim_promotion) 2>"$temporary_directory/promotion.stderr"; then
  fail 'promotion retry became fail-open'
fi
[[ $(<"$bridge_count_file") == 14 ]] || fail 'promotion attempt limit drifted'
[[ $(grep -Fc "$CLAIM_RETRY_DIAGNOSTIC_PREFIX" "$temporary_directory/promotion.stderr") == 4 ]] ||
  fail 'promotion diagnostic rate limit drifted'
grep -Fq 'operation=claim-promotion attempt=14/14 class=local-configuration' \
  "$temporary_directory/promotion.stderr" || fail 'last promotion error was not classified'
grep -Fq 'owner promotion work is unavailable' "$temporary_directory/promotion.stderr" ||
  fail 'promotion timeout did not fail closed'

if grep -R -Fq 'raw-secret-must-not-leak' \
  "$temporary_directory/admission.stderr" "$temporary_directory/promotion.stderr"; then
  fail 'raw bridge stderr reached operator diagnostics'
fi
if grep -vxq '600' "$stderr_mode_file"; then
  fail 'bridge stderr capture is not private'
fi

long_value=$(printf 'x%.0s' {1..400})
emit_claim_retry_diagnostic "$long_value" 1 "$long_value" 2>"$temporary_directory/bounded.stderr"
[[ $(wc -c <"$temporary_directory/bounded.stderr") -le $((CLAIM_RETRY_DIAGNOSTIC_MAX_BYTES + 1)) ]] ||
  fail 'operator diagnostic exceeded its byte bound'

printf 'Image admission retry diagnostics test passed\n'
