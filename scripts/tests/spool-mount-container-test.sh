#!/usr/bin/env bash
set -euo pipefail

# Только заранее подготовленный локальный образ; без pull, сети и данных контура.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=${KODEX_SPOOL_TEST_IMAGE:?KODEX_SPOOL_TEST_IMAGE is required}
[[ "$image" =~ ^sha256:[a-f0-9]{64}$ ]] || exit 1
docker image inspect "$image" >/dev/null
temporary_root=$(mktemp -d)
container_name="kodex-spool-boundary-$$"
cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  docker run --rm --pull=never --network none --user 0:0 --entrypoint /bin/sh \
    --mount "type=bind,src=$temporary_root,dst=/fixture" "$image" -ec 'rm -rf /fixture/spool' >/dev/null 2>&1 || true
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT
mkdir "$temporary_root/authority"
chmod 0755 "$temporary_root"
# DAC разрешает запись: отрицательный контроль обязан упереться именно в RO mount.
chmod 0777 "$temporary_root/authority"
# Тот же init executable, затем настоящий spool constructor/readiness.
# Root используется только для воспроизведения kubelet ownership пустого fixture.
mkdir "$temporary_root/spool"
(cd "$repository_root/services/internal/runtime-controller"
  CGO_ENABLED=0 GOWORK=off timeout 120s go build -trimpath -buildvcs=false -o "$temporary_root/controller" ./cmd/runtime-controller
  CGO_ENABLED=0 GOWORK=off timeout 120s go test -c -o "$temporary_root/callback.test" ./internal/callback)
chmod 0555 "$temporary_root/controller" "$temporary_root/callback.test"
timeout 20s docker run --rm --pull=never --network none --user 0:0 \
  --mount "type=bind,src=$temporary_root/spool,dst=/spool" \
  --entrypoint /bin/sh "$image" -ec 'chown 0:29000 /spool; chmod 2777 /spool; test "$(stat -c %a:%u:%g /spool)" = 2777:0:29000'
for attempt in first replay; do
  timeout 20s docker run --rm --name "$container_name" --pull=never --network none \
    --read-only --user 10001:10001 --group-add 29000 --cap-drop ALL --security-opt no-new-privileges \
    --mount "type=bind,src=$temporary_root/controller,dst=/controller,readonly" \
    --mount "type=bind,src=$temporary_root/spool,dst=/spool" \
    --entrypoint /controller "$image" --prepare-artifact-spool /spool
done
timeout 20s docker run --rm --name "$container_name" --pull=never --network none \
  --read-only --user 10001:10001 --group-add 29000 --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=bind,src=$temporary_root/callback.test,dst=/callback.test,readonly" \
  --mount "type=bind,src=$temporary_root/spool/controller,dst=/spool" \
  --mount "type=bind,src=$temporary_root/authority,dst=/run/kodex,readonly" \
  -e KODEX_ARTIFACT_SPOOL_CONTAINER_DIRECTORY=/spool --entrypoint /callback.test "$image" \
  -test.run '^TestArtifactSpoolContainerStartup$' -test.timeout 10s -test.v
# Чужой UID существующего child нельзя исправить либо принять повторным init.
timeout 20s docker run --rm --pull=never --network none --user 0:0 \
  --mount "type=bind,src=$temporary_root/spool,dst=/spool" \
  --entrypoint /bin/sh "$image" -ec 'chown 10002:10002 /spool/controller'
negative_status=0
timeout 20s docker run --rm --name "$container_name" --pull=never --network none \
  --read-only --user 10001:10001 --group-add 29000 --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=bind,src=$temporary_root/controller,dst=/controller,readonly" \
  --mount "type=bind,src=$temporary_root/spool,dst=/spool" \
  --entrypoint /controller "$image" --prepare-artifact-spool /spool || negative_status=$?
[[ "$negative_status" == 1 ]] || { printf 'Foreign spool rejection did not exit exactly 1\n' >&2; exit 1; }
for component in runtime-controller stt-tts-service; do
  if [[ "$component" == runtime-controller ]]; then
    key=RUNTIME_CONTROLLER_ARTIFACT_SPOOL_DIRECTORY
  else
    key=STT_SPOOL_DIRECTORY
  fi
  spool=$(yq -r ".data.$key" "$repository_root/deploy/k8s/base/$component/configmap.yaml")
  timeout 20s docker run --rm --name "$container_name" --pull=never --network none \
    --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges \
    --mount "type=bind,src=$temporary_root/authority,dst=/run/kodex,readonly" \
    --tmpfs "$spool:rw,nosuid,nodev,noexec,size=1048576,mode=0700,uid=10001,gid=10001" \
    --entrypoint /bin/sh "$image" -ec '
      test "$(readlink -f /var/run)" = /run
      printf proof > "$1/proof"
      test "$(cat "$1/proof")" = proof
      if touch /run/kodex/forbidden 2>/dev/null; then exit 1; fi
      rm "$1/proof"
    ' sh "$spool"
done
printf 'Container spool write and read-only authority checks passed\n'
