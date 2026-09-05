#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
cd -- "$root"
oapi-codegen -config tools/codegen/openapi/email-bridge-go.yaml \
  -o "$temporary/api.gen.go" contracts/openapi/email-bridge/v1/openapi.yaml
cmp "$temporary/api.gen.go" libs/go/emailbridgeapi/api.gen.go
node tools/codegen/email-bridge-schema.mjs --check
printf 'Email bridge contract codegen passed\n'
