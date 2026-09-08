#!/usr/bin/env bash
set -euo pipefail
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
legacy_revision=d21a024dbf03fca13b20f0cb30d858d743729ed7
fixture_directory=$(mktemp -d /tmp/elc.XXXXXX)
trap 'rm -rf -- "$fixture_directory"' EXIT
chmod 0700 "$fixture_directory"
export GOMAXPROCS=4 TMPDIR=/tmp
export KODEX_EMAIL_GATEWAY_TEST_BINARY="$fixture_directory/gateway.test"
export KODEX_EMAIL_CASTER_TEST_BINARY="$fixture_directory/caster.test"
GOWORK=off go -C "$repository_root/services/external/integration-gateway" test -c -p 2 -o "$KODEX_EMAIL_GATEWAY_TEST_BINARY" ./internal/integration
GOWORK=off go -C "$repository_root/services/internal/control-plane" test -c -p 2 -o "$KODEX_EMAIL_CASTER_TEST_BINARY" ./internal/transport/grpc
mkdir "$fixture_directory/source"
git -C "$repository_root" archive "$legacy_revision" | tar -x -C "$fixture_directory/source"
python3 "$repository_root/scripts/tests/email-legacy-test-overlay.py" "$repository_root" "$fixture_directory/source" "$legacy_revision"
# Один собственный PostgreSQL контейнер, dynamic loopback port, полный старый
# canonical профиль. Устанавливаем только test hook; реальные провайдеры не вызываются.
"$fixture_directory/source/scripts/tests/control-plane-postgres-test.sh"
