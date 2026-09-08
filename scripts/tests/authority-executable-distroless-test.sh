#!/usr/bin/env bash
set -euo pipefail

repository=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
temporary=$(mktemp -d /tmp/kodex-authority-executable-XXXXXX)
image_tag="kodex-authority-executable-test:$(basename "$temporary" | tr '[:upper:]' '[:lower:]')"
containers=()
cleanup() {
  for container in "${containers[@]}"; do docker rm -f "$container" >/dev/null 2>&1 || true; done
  docker image rm "$image_tag" >/dev/null 2>&1 || true
  rm -rf "$temporary"
}
trap cleanup EXIT
export GOWORK=off GOMAXPROCS=4 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
go -C "$repository/services/internal/internal-rpc-authority" build -p 2 -trimpath -buildvcs=false \
  -ldflags='-s -w' -o "$temporary/proof" ./cmd/internal-rpc-authority-executable-proof
go build -p 2 -trimpath -buildvcs=false -o "$temporary/process" "$repository/scripts/tests/fixtures/authority-executable-process.go"
runtime_base=$(awk '/^FROM gcr.io\/distroless\/static-debian12:nonroot@sha256:/ {print $2}' "$repository/services/internal/internal-rpc-authority/Dockerfile")
[[ "$runtime_base" =~ ^gcr.io/distroless/static-debian12:nonroot@sha256:[a-f0-9]{64}$ ]]
cat >"$temporary/Dockerfile" <<EOF
FROM $runtime_base
COPY --chmod=0555 proof /usr/local/bin/internal-rpc-authority-executable-proof
COPY --chmod=0555 process /usr/local/bin/internal-rpc-authority-issuer
COPY --chmod=0555 process /usr/local/bin/internal-rpc-authority-verifier
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/internal-rpc-authority-issuer"]
EOF
docker build --quiet --tag "$image_tag" "$temporary" >/dev/null
expected=$(sha256sum "$temporary/process" | awk '{print $1}')
for mode in single verifier double; do
  role=issuer
  if [[ "$mode" == verifier ]]; then role=verifier; fi
  container="$(basename "$temporary" | tr '[:upper:]' '[:lower:]')-$mode"
  containers+=("$container")
  docker run --detach --name "$container" --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges --pids-limit 64 --memory 64m --cpus 1 --user 29001:29000 \
    --entrypoint "/usr/local/bin/internal-rpc-authority-$role" "$image_tag" "$mode" >/dev/null
  ready=false
  for _ in {1..50}; do
    count=$(docker logs "$container" 2>/dev/null | awk '/^ready$/ {n++} END {print n+0}')
    if [[ "$mode" != double && "$count" == 1 ]] || [[ "$mode" == double && "$count" == 2 ]]; then ready=true; break; fi
    sleep 0.1
  done
  [[ "$ready" == true ]]
  if docker exec "$container" sh -c true >"$temporary/shell.log" 2>&1; then
    printf '%s\n' 'Distroless shell unexpectedly available' >&2; exit 1
  fi
  if [[ "$mode" != double ]]; then
    docker exec "$container" /usr/local/bin/internal-rpc-authority-executable-proof --role "$role" >"$temporary/proof.json"
    jq -e --arg hash "$expected" --arg role "$role" '.version == 1 and .role == $role and .pid == 1 and (.startTicks | test("^[0-9]+$")) and .binarySHA256 == $hash' "$temporary/proof.json" >/dev/null
    missing=verifier
    if [[ "$role" == verifier ]]; then missing=issuer; fi
    for invalid_role in "$missing" publisher; do
      if docker exec "$container" /usr/local/bin/internal-rpc-authority-executable-proof --role "$invalid_role" >"$temporary/negative.log" 2>&1; then
        printf '%s\n' 'Missing or unsupported authority process accepted' >&2; exit 1
      fi
    done
  elif docker exec "$container" /usr/local/bin/internal-rpc-authority-executable-proof --role issuer >"$temporary/duplicate.log" 2>&1; then
    printf '%s\n' 'Duplicate authority process accepted' >&2; exit 1
  fi
done
printf '%s\n' 'PASS: native distroless executable digest, no shell, missing/unsupported/duplicate process rejected'
