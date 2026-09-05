#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
(
  cd "$root/services/internal/secret-broker"
  go test -race -timeout 120s ./...
  go vet ./...
  go build ./cmd/...
)
python3 "$root/scripts/tests/secret-drafts-bootstrap-test.py"
python3 "$root/scripts/tests/secret-drafts-render-test.py"
