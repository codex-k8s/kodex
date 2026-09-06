#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
for command in docker yq go timeout; do
  command -v "$command" >/dev/null || { echo "Required component tool is unavailable" >&2; exit 1; }
done
image=$(yq -r '.spec.template.spec.containers[] | select(.name == "nats") | .image' "$root/deploy/k8s/base/platform-state/nats.yaml")
[[ "$image" =~ ^docker\.io/library/nats:[0-9.]+-alpine@sha256:[a-f0-9]{64}$ ]] || { echo "Canonical NATS image is not pinned" >&2; exit 1; }
fixture=$(mktemp -d "${TMPDIR:-/tmp}/browser-state.XXXXXX")
container="kodex-browser-state-$(basename "$fixture")"
volume="$container-data"
passed=false
cleanup() {
  docker logs "$container" >"$fixture/nats.log" 2>&1 || true
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker volume rm "$volume" >/dev/null 2>&1 || true
  if [[ "$passed" == true ]]; then rm -rf -- "$fixture"; else echo "Component evidence retained at $fixture" >&2; fi
}
trap cleanup EXIT
docker volume create "$volume" >/dev/null
if ! docker image inspect "$image" >/dev/null 2>&1; then
  timeout 90s docker pull "$image" >"$fixture/pull.log" 2>&1
fi
docker create --name "$container" --user 0:0 \
  --cpus 2 --memory 256m --pids-limit 64 --read-only --cap-drop ALL \
  --security-opt no-new-privileges --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --publish 127.0.0.1::4222 --mount "type=volume,src=$volume,dst=/data" \
  "$image" -js -sd /data -a 0.0.0.0 -p 4222 >"$fixture/container.log"
docker start "$container" >/dev/null
address=$(docker port "$container" 4222/tcp)
[[ "$address" =~ ^127\.0\.0\.1:[0-9]+$ ]] || { echo "Disposable NATS binding is invalid" >&2; exit 1; }
run_phase() {
  (
    cd "$root/libs/go/eventing"
    KODEX_BROWSER_STATE_TEST_URL="nats://$address" KODEX_BROWSER_STATE_TEST_PHASE="$1" \
      GOMAXPROCS=2 timeout 60s go test -p 1 -count=1 -race -timeout 45s ./browserstate -run '^TestBrowserStateComponent$'
  )
}
run_phase write
docker kill --signal KILL "$container" >/dev/null
docker start "$container" >/dev/null
address=$(docker port "$container" 4222/tcp)
[[ "$address" =~ ^127\.0\.0\.1:[0-9]+$ ]] || { echo "Restarted NATS binding is invalid" >&2; exit 1; }
run_phase read
passed=true
echo "Browser state FileStorage, CAS, lost ACK, restart and readiness checks passed"
