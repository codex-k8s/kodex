#!/usr/bin/env bash
set -euo pipefail
umask 077
# Только synthetic TLS/CONNECT; child binary не получает launcher credentials.
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
test_environment=(env -i "HOME=$HOME" "PATH=$PATH" GOENV=off GOWORK=off GOTOOLCHAIN=local "GOMAXPROCS=${GOMAXPROCS:-2}")
for key in GOROOT GOCACHE GOTMPDIR TMPDIR; do
  [[ -z "${!key:-}" ]] || test_environment+=("$key=${!key}")
done
(
  cd "$root/services/external/egress-gateway"
  "${test_environment[@]}" timeout 180s go test -p 1 -c -race -o "$temporary/native-gateway.test" ./internal/gateway
)
(
  cd "$root/services/external/interaction-gateway"
  "${test_environment[@]}" "KODEX_TEST_NATIVE_GATEWAY_BINARY=$temporary/native-gateway.test" \
    timeout 180s go test -p 1 -race -count=1 -timeout=150s -run '^TestNativeTelegramHTTPSUsesCONNECTAndDefaultTransport$' ./internal/telegram
)
