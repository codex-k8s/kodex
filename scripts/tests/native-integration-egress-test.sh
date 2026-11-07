#!/usr/bin/env bash
set -euo pipefail
# Только synthetic qualification: child binaries не получают launcher credentials.
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
test_environment=(env -i "HOME=$HOME" "PATH=$PATH" GOENV=off GOWORK=off)
[[ -z "${GOROOT:-}" ]] || test_environment+=("GOROOT=$GOROOT")
(
  cd "$root/services/external/egress-gateway"
  "${test_environment[@]}" timeout 180s go test -c -race -o "$temporary/native-gateway.test" ./internal/gateway
)
(
  cd "$root/services/external/integration-gateway"
  "${test_environment[@]}" "KODEX_TEST_NATIVE_GATEWAY_BINARY=$temporary/native-gateway.test" \
    timeout 180s go test -race -count=1 -timeout=150s -run '^TestNativeHTTPSProcessKeepsTypedAdapterCredentialsAndTLS$' ./internal/integration
)
