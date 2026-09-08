#!/usr/bin/env bash
set -euo pipefail
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
fixture_directory=$(mktemp -d /tmp/emc.XXXXXX)
trap 'rm -rf -- "$fixture_directory"' EXIT
chmod 0700 "$fixture_directory"
export GOMAXPROCS=4 TMPDIR=/tmp GOFLAGS='-p=2'
GOWORK=off go -C "$repository_root/services/external/integration-gateway" test -c -p 2 -o "$fixture_directory/gateway.test" ./internal/integration
export KODEX_EMAIL_GATEWAY_TEST_BINARY="$fixture_directory/gateway.test"
# Собственный контейнер и dynamic loopback port создаёт только канонический harness.
"$repository_root/scripts/tests/control-plane-postgres-test.sh"
