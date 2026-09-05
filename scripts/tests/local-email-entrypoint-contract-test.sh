#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render="$temporary_directory/render.yaml"
command_log="$temporary_directory/commands"
# Глобальная переменная читается извлечённой функцией deploy-local.
# shellcheck disable=SC2034
namespace=kodex-system
export command_log
fail() { printf 'Local EMAIL entrypoint contract failed: %s\n' "$*" >&2; exit 1; }
printf '%s\n' '{"kind":"Job","metadata":{"name":"email-bridge-migration"}}' >"$render"
# Проверяется настоящая функция entrypoint, kubectl намеренно недоступен.
kubectl() { printf '%s\n' "$*" >>"$command_log"; }
wait_job() { printf 'wait %s\n' "$*" >>"$command_log"; return "${wait_result:-0}"; }
# shellcheck disable=SC1090
source <(awk '
  /^apply_job\(\) \{/ { capture=1 }
  capture { print }
  capture && /^}$/ { exit }
' "$root/tools/dev/deploy-local.sh")
apply_job email-bridge-migration
test "$(wc -l <"$command_log")" = 3 || fail 'unexpected migration operations'
rg -Fxq -- '-n kodex-system delete job/email-bridge-migration --ignore-not-found --wait=true --timeout=3m' "$command_log" || fail 'bounded exact deletion is absent'
rg -Fxq -- "apply --server-side --force-conflicts --field-manager=kodex-local-dev -f $temporary_directory/job-email-bridge-migration.yaml" "$command_log" || fail 'exact migration apply is absent'
test "$(tail -n 1 "$command_log")" = 'wait email-bridge-migration' || fail 'migration completion is not awaited'
wait_result=1
if apply_job email-bridge-migration; then
  fail 'migration failure was ignored'
fi

node - "$root/tools/dev/deploy-local.sh" <<'JS'
const fs = require('fs');
const source = fs.readFileSync(process.argv[2], 'utf8');
const ordered = [
  '  wait_certificates\n',
  '  apply_render statefulsets ',
  '  for workload in kodex-postgresql kodex-nats seaweedfs email-bridge-postgresql; do',
  'rollout status "statefulset/$workload" --timeout=10m',
  '  apply_job email-bridge-migration\n',
  '  apply_render authority-publisher ',
  '  apply_render application-workloads '
];
let offset = 0;
for (const marker of ordered) {
  const found = source.indexOf(marker, offset);
  if (found < 0) throw new Error('EMAIL startup ordering contract is absent');
  offset = found + marker.length;
}
if (!source.includes('email-bridge-migration kodex-postgresql-runtime-credentials')) {
  throw new Error('EMAIL migration final readback is absent');
}
JS
printf 'Local EMAIL entrypoint contract passed: exact migration, failure and DB/startup ordering\n'
