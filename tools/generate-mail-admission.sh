#!/usr/bin/env bash
set -euo pipefail
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
temporary=$(mktemp)
trap 'rm -f -- "$temporary"' EXIT
(cd "$repository_root/services/external/egress-gateway" && go run ./cmd/mail-admission) >"$temporary"
chmod 0644 "$temporary"
mv -- "$temporary" "$repository_root/deploy/k8s/base/egress-gateway/mail/publication-admission.json"
