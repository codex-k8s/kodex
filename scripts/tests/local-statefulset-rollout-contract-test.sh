#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local StatefulSet rollout contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fake_bin="$temporary_directory/bin"
command_log="$temporary_directory/kubectl.log"
replacement_state="$temporary_directory/replaced"
mkdir -p -- "$fake_bin"

cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${KODEX_TEST_COMMAND_LOG:?}"
case "$*" in
  '-n kodex-system get statefulset/kodex-postgresql -o json')
    printf '%s\n' '{"status":{"currentRevision":"old","updateRevision":"new"}}'
    ;;
  '-n kodex-system get pods -o json')
    printf '%s\n' '{"items":[{"metadata":{"name":"kodex-postgresql-0","uid":"old-uid","ownerReferences":[{"kind":"StatefulSet","name":"kodex-postgresql"}]}}]}'
    ;;
  '-n kodex-system delete pod/kodex-postgresql-0 --ignore-not-found --wait=false')
    : >"${KODEX_TEST_REPLACEMENT_STATE:?}"
    ;;
  '-n kodex-system get pod/kodex-postgresql-0 -o json')
    if [[ -e ${KODEX_TEST_REPLACEMENT_STATE:?} ]]; then
      printf '%s\n' '{"metadata":{"name":"kodex-postgresql-0","uid":"new-uid"}}'
    else
      printf '%s\n' '{"metadata":{"name":"kodex-postgresql-0","uid":"old-uid"}}'
    fi
    ;;
  *)
    printf 'unexpected kubectl call: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF
chmod +x -- "$fake_bin/kubectl"

extract_function() {
  local function_name=$1
  awk -v signature="${function_name}() {" '
    $0 == signature { capture = 1 }
    capture { print }
    capture && /^}$/ { exit }
  ' "$repository_root/tools/dev/deploy-local.sh"
}

export PATH="$fake_bin:$PATH"
export KODEX_TEST_COMMAND_LOG="$command_log"
export KODEX_TEST_REPLACEMENT_STATE="$replacement_state"
# The sourced deploy functions read this global namespace.
# shellcheck disable=SC2034
namespace=kodex-system
# shellcheck disable=SC1090
source <(extract_function wait_for_pod_uid_replacement)
# shellcheck disable=SC1090
source <(extract_function reconcile_local_statefulset_rollout)

reconcile_local_statefulset_rollout kodex-postgresql

grep -Fq -- '-n kodex-system delete pod/kodex-postgresql-0 --ignore-not-found --wait=false' \
  "$command_log" || fail 'outdated Pod deletion is not non-blocking'
grep -Fq -- '-n kodex-system get pod/kodex-postgresql-0 -o json' "$command_log" ||
  fail 'replacement was not distinguished by Pod UID'
if grep -Fq -- '--wait=true' "$command_log"; then
  fail 'outdated Pod deletion still waits on a reusable StatefulSet name'
fi

printf 'Local StatefulSet rollout contract tests passed\n'
