#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Service infrastructure bootstrap test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/service-infrastructure/bootstrap.sh"
lock="$repository_root/infra/service-infrastructure/charts.lock.json"

bash -n "$bootstrap"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 1 and
  .charts[0].name == "trust-manager" and .charts[0].chart == "trust-manager" and
  (.charts[0].version | test("^v?[0-9]+\\.[0-9]+\\.[0-9]+$")) and
  (.charts[0].sha256 | test("^[a-f0-9]{64}$"))
' "$lock" >/dev/null || fail 'trust-manager chart lock is invalid'
rg -q 'preflight\|apply-controllers\|readback' "$bootstrap" ||
  fail 'bootstrap modes are incomplete'
if rg -qi 'vault|secrets-store|SecretProviderClass' \
  "$repository_root/infra/service-infrastructure"; then
  fail 'retired secret infrastructure remains active'
fi
printf 'Service infrastructure bootstrap test completed\n'
