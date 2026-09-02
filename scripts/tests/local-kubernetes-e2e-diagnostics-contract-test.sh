#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local Kubernetes E2E diagnostics contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fake_bin="$temporary_directory/bin"
command_log="$temporary_directory/kubectl.log"
mkdir -p -- "$fake_bin"

cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
case "$*" in
  '-n kodex-system get jobs -l '*'-o json')
    case "${KODEX_TEST_INVENTORY_MODE:?}" in
      terminal)
        cat <<'JSON'
{"items":[{"metadata":{"namespace":"kodex-system","name":"backup-controller-e2e-restore-20260902123456-42","uid":"job-uid","labels":{"app.kubernetes.io/part-of":"kodex","app.kubernetes.io/managed-by":"kodex-local-e2e","kodex.dev/local-profile":"hot-reload","kodex.dev/e2e-run":"20260902123456-42"}},"spec":{"activeDeadlineSeconds":900},"status":{"failed":1,"conditions":[{"type":"Failed","status":"True","reason":"BackoffLimitExceeded"}]}}]}
JSON
        ;;
      nonterminal)
        cat <<'JSON'
{"items":[{"metadata":{"namespace":"kodex-system","name":"backup-controller-e2e-restore-20260902123456-42","uid":"job-uid","labels":{"app.kubernetes.io/part-of":"kodex","app.kubernetes.io/managed-by":"kodex-local-e2e","kodex.dev/local-profile":"hot-reload","kodex.dev/e2e-run":"20260902123456-42"}},"spec":{"activeDeadlineSeconds":900},"status":{"active":1}}]}
JSON
        ;;
      unsafe)
        cat <<'JSON'
{"items":[{"metadata":{"namespace":"kodex-system","name":"backup-controller-e2e-restore-20260902123456-42","uid":"job-uid","labels":{"app.kubernetes.io/part-of":"kodex","app.kubernetes.io/managed-by":"foreign-controller","kodex.dev/local-profile":"hot-reload","kodex.dev/e2e-run":"20260902123456-42"}},"spec":{"activeDeadlineSeconds":900},"status":{"failed":1,"conditions":[{"type":"Failed","status":"True"}]}}]}
JSON
        ;;
    esac
    ;;
  '-n kodex-system get pods -l job-name=backup-controller-e2e-restore-20260902123456-42 -o json')
    cat <<'JSON'
{"items":[{"metadata":{"namespace":"kodex-system","name":"restore-pod","uid":"pod-uid","ownerReferences":[{"apiVersion":"batch/v1","kind":"Job","uid":"job-uid","controller":true}]},"spec":{"initContainers":[{"name":"init"}],"containers":[{"name":"restore"}]},"status":{"phase":"Failed"}}]}
JSON
    ;;
  '-n kodex-system logs restore-pod -c '*'--tail=500 --limit-bytes=262144')
    cat <<'LOG'
normal diagnostic line
Authorization: Bearer fixture-bearer-value
password=short-secret
postgres://owner:database-password@postgres:5432/kodex
long=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789
LOG
    ;;
  '-n kodex-system get events --field-selector involvedObject.uid=pod-uid -o json')
    printf '%s\n' '{"items":[{"type":"Warning","reason":"Failed","action":"Start","message":"token=must-not-survive","count":1,"eventTime":"2026-09-02T12:00:00Z"}]}'
    ;;
  '-n kodex-system patch job/backup-controller-e2e-restore-20260902123456-42 --type=merge -p {"spec":{"ttlSecondsAfterFinished":1800}}')
    ;;
  *)
    printf 'unexpected kubectl call: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF
chmod +x -- "$fake_bin/kubectl"

export PATH="$fake_bin:$PATH"
export KODEX_TEST_COMMAND_LOG="$command_log"
source "$repository_root/scripts/tests/lib/local-kubernetes-e2e.sh"

cleanup_contract=$(sed -n '/^cleanup() {$/,/^}$/p' \
  "$repository_root/scripts/tests/local-backup-restore-e2e.sh")
secret_cleanup_line=$(grep -n 'delete secret/backup-controller-repository' <<<"$cleanup_contract" | cut -d: -f1)
database_cleanup_line=$(grep -n 'DROP DATABASE IF EXISTS' <<<"$cleanup_contract" | cut -d: -f1)
credential_cleanup_line=$(grep -nF "rm -rf -- \"\$temporary_directory\"" \
  <<<"$cleanup_contract" | cut -d: -f1)
retention_line=$(grep -n 'kodex_e2e_retain_owned_terminal_jobs_on_failure' <<<"$cleanup_contract" | cut -d: -f1)
[[ -n "$secret_cleanup_line" && -n "$database_cleanup_line" && -n "$credential_cleanup_line" &&
  -n "$retention_line" && "$secret_cleanup_line" -lt "$retention_line" &&
  "$database_cleanup_line" -lt "$retention_line" && "$credential_cleanup_line" -lt "$retention_line" ]] ||
  fail 'secret, credential, or database cleanup does not precede failure retention'

selector='app.kubernetes.io/name=backup-controller,kodex.dev/e2e-run=20260902123456-42'
name_pattern='^backup-controller-e2e-restore-[0-9]{14}-[0-9]+$'
bundle="$temporary_directory/bundle"
export KODEX_TEST_INVENTORY_MODE=terminal
kodex_e2e_retain_owned_terminal_jobs_on_failure kodex-system "$selector" "$name_pattern" "$bundle"

[[ -f "$bundle/inventory.json" && "$(stat -c '%a' "$bundle")" == 700 &&
  "$(stat -c '%a' "$bundle/inventory.json")" == 600 ]] ||
  fail 'private diagnostics bundle was not created'
jq -e '
  .version == 1 and .namespace == "kodex-system" and
  .retentionSecondsAfterFinished == 1800 and
  .jobs == [{
    name:"backup-controller-e2e-restore-20260902123456-42",
    uid:"job-uid",
    status:{active:0,failed:1,succeeded:0,conditions:[{type:"Failed",status:"True",reason:"BackoffLimitExceeded"}]}
  }]
' "$bundle/inventory.json" >/dev/null || fail 'sanitized Job inventory is invalid'
grep -Fq 'normal diagnostic line' "$bundle/logs/restore-pod/restore.log" ||
  fail 'bounded container log was not retained'
if grep -REn 'fixture-bearer|short-secret|database-password|ABCDEFGHIJKLMNOPQRSTUVWXYZ|must-not-survive' "$bundle" >/dev/null; then
  fail 'diagnostics bundle retained credential-like content'
fi
grep -Fq 'ttlSecondsAfterFinished":1800' "$command_log" ||
  fail 'terminal Job retention TTL was not patched to 1800 seconds'

: >"$command_log"
export KODEX_TEST_INVENTORY_MODE=nonterminal
kodex_e2e_retain_owned_terminal_jobs_on_failure kodex-system "$selector" "$name_pattern" \
  "$temporary_directory/nonterminal"
[[ ! -e "$temporary_directory/nonterminal" ]] || fail 'non-terminal Job was retained'
if grep -q ' patch ' "$command_log"; then
  fail 'non-terminal Job TTL was changed'
fi

: >"$command_log"
export KODEX_TEST_INVENTORY_MODE=unsafe
if kodex_e2e_retain_owned_terminal_jobs_on_failure kodex-system "$selector" "$name_pattern" \
  "$temporary_directory/unsafe"; then
  fail 'foreign-owned Job was accepted for diagnostics retention'
fi
[[ ! -e "$temporary_directory/unsafe" ]] || fail 'foreign-owned Job produced a diagnostics bundle'
if grep -q ' patch ' "$command_log"; then
  fail 'foreign-owned Job TTL was changed'
fi

: >"$command_log"
if kodex_e2e_retain_owned_terminal_jobs_on_failure kodex-system \
  'app.kubernetes.io/name=backup-controller' "$name_pattern" \
  "$temporary_directory/broad-selector"; then
  fail 'selector without an exact E2E run identity was accepted'
fi
[[ ! -s "$command_log" ]] || fail 'unsafe broad selector reached Kubernetes API'

printf 'Local Kubernetes E2E diagnostics contract tests passed\n'
