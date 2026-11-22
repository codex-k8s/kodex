#!/usr/bin/env bash
set -euo pipefail
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
go_parallelism=${KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM:-2}
[[ "$go_parallelism" =~ ^[12]$ ]] || { printf 'Personal email fixture requires Go parallelism 1 or 2\n' >&2; exit 1; }
[[ ! -v KODEX_EMAIL_GATEWAY_TEST_BINARY ]] || { printf 'External gateway test binary is forbidden\n' >&2; exit 1; }
fixture_directory=$(mktemp -d "${TMPDIR:-/tmp}/personal-email-owner.XXXXXXXX")
chmod 0700 "$fixture_directory"
trap 'rm -rf -- "$fixture_directory"' EXIT
# Новый профиль обязательно потребляет actual owner claims через штатный Proto caster
# и текущий gateway adapter. Старые optional fixtures остаются без изменений.
env -u GOFLAGS GOENV=off GOWORK=off go -C "$repository_root/services/external/integration-gateway"   test -c -race -p "$go_parallelism" -o "$fixture_directory/gateway.test" ./internal/integration
export KODEX_EMAIL_GATEWAY_TEST_BINARY="$fixture_directory/gateway.test"
unset KODEX_EMAIL_LEGACY_CLAIM KODEX_EMAIL_CLAIM_KIND
# Один cached disposable PG принадлежит каноническому harness; это development path,
# а не подмена закрытых047/048→049 full-schema proofs или policy110 transport.
bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" --personal-email-owner-development
