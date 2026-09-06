#!/usr/bin/env bash
set -euo pipefail

# Только заранее подготовленный локальный образ; без pull, сети и данных контура.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=${KODEX_SPOOL_TEST_IMAGE:?KODEX_SPOOL_TEST_IMAGE is required}
[[ "$image" =~ ^sha256:[a-f0-9]{64}$ ]] || exit 1
docker image inspect "$image" >/dev/null
temporary_root=$(mktemp -d)
container_name="kodex-spool-boundary-$$"
trap 'docker rm -f "$container_name" >/dev/null 2>&1 || true; rm -rf -- "$temporary_root"' EXIT
mkdir "$temporary_root/authority"
chmod 0755 "$temporary_root"
# DAC разрешает запись: отрицательный контроль обязан упереться именно в RO mount.
chmod 0777 "$temporary_root/authority"
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
