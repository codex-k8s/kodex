#!/usr/bin/env bash
set -euo pipefail

# Только local Unix Docker, собственные disposable volumes/containers и уже cached public image.
# Квалификация компонентов не собирает и не публикует deployment RoleImage.
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
endpoint=$(docker context inspect --format '{{(index .Endpoints "docker").Host}}')
[[ "$endpoint" == unix:///* && -z "${DOCKER_HOST:-}" ]] || { printf 'Local Unix Docker is required\n' >&2; exit 1; }
base='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
# Версия совпадает с общей PostgreSQL-оснасткой; ниже проверяется только локальное наличие.
base_id=$(docker image inspect --format '{{.Id}}' "$base")
[[ "$base_id" == sha256:* ]] || exit 1
prefix="kodex-workspace-fixture-$$"
fixture_dir=$(mktemp -d)
seed=''
daemon=''
image="${prefix}:local"
work="${prefix}-work"
socket="${prefix}-socket"
cleanup() {
  [[ -z "$daemon" ]] || docker rm -f -v "$daemon" >/dev/null 2>&1 || true
  [[ -z "$seed" ]] || docker rm -f -v "$seed" >/dev/null 2>&1 || true
  docker rm -f -v "${prefix}-inherited-init" >/dev/null 2>&1 || true
  docker image rm "$image" >/dev/null 2>&1 || true
  docker volume rm "$work" "$socket" >/dev/null 2>&1 || true
  rm -rf -- "$fixture_dir"
}
trap cleanup EXIT
chmod 0700 "$fixture_dir"
# Точный штатный init берётся из Dockerfile; PostgreSQL base не подменяет его поведение.
python3 - "$root" "$fixture_dir" <<'PYINIT'
import base64, pathlib, re, sys
source = (pathlib.Path(sys.argv[1]) / 'services/jobs/agent-runner/Dockerfile').read_text()
encoded = re.search(r"printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d", source).group(1)
(pathlib.Path(sys.argv[2]) / 'init.go').write_bytes(base64.b64decode(encoded, validate=True))
PYINIT
(
  cd -- "$root/services/jobs/agent-runner"
  CGO_ENABLED=0 GOWORK=off GOENV=off go build -o "$fixture_dir/kodex-init" "$fixture_dir/init.go"
  CGO_ENABLED=0 GOWORK=off GOENV=off go build -o "$fixture_dir/kodex-agent-runner" ./cmd/agent-runner
  CGO_ENABLED=0 GOWORK=off GOENV=off go build -o "$fixture_dir/probe" ./testdata/workspace_executor_probe.go
)
seed=$(docker run -d --network none --user 0 --entrypoint /bin/sh "$base_id" -c "sleep 120")
docker cp "$fixture_dir/kodex-init" "$seed:/usr/local/bin/kodex-init"
docker cp "$fixture_dir/kodex-agent-runner" "$seed:/usr/local/bin/kodex-agent-runner"
docker cp "$fixture_dir/probe" "$seed:/usr/local/bin/workspace-executor-probe"
docker exec --user 0 "$seed" /bin/sh -c 'chown 0:0 /usr/local/bin/kodex-init /usr/local/bin/kodex-agent-runner /usr/local/bin/workspace-executor-probe; chmod 0555 /usr/local/bin/kodex-init /usr/local/bin/kodex-agent-runner /usr/local/bin/workspace-executor-probe'
docker commit --change 'ENTRYPOINT ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"]' --change 'USER 10002:29000' --change 'LABEL kodex.dev.test-only=true' "$seed" "$image" >/dev/null
# Наследуемый production init оставляет runner дочерним PID: guard обязан это отвергнуть.
if timeout 10s docker run --rm --name "${prefix}-inherited-init" --network none --read-only \
  --user 10002:29000 --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 64 --memory 128m "$image" runtime-workspace-executor \
  >"$fixture_dir/inherited-init.log" 2>&1; then
  printf 'Inherited init unexpectedly passed the executor PID1 guard\n' >&2; exit 1
fi
grep -Fq 'managed workspace command is unavailable' "$fixture_dir/inherited-init.log" || {
  printf 'Inherited init did not reach the protected PID1 guard\n' >&2; exit 1;
}
docker volume create "$work" >/dev/null
docker volume create "$socket" >/dev/null
docker run --rm --network none --user 0 --entrypoint /bin/sh \
  --mount "type=volume,src=$work,dst=/fixture/work" --mount "type=volume,src=$socket,dst=/fixture/socket" \
  "$image" -c 'chown 10001:29000 /fixture/work; chmod 2770 /fixture/work; chown 0:29000 /fixture/socket; chmod 2770 /fixture/socket'
daemon=$(docker run -d --network none --read-only --user 10002:29000 --cap-drop ALL \
  --entrypoint /usr/local/bin/kodex-agent-runner \
  --security-opt no-new-privileges --pids-limit 256 --memory 256m --cpus 1 \
  --tmpfs /tmp:rw,nosuid,nodev,size=67108864,mode=1777 \
  --mount "type=volume,src=$work,dst=/workspace/work" --mount "type=volume,src=$socket,dst=/run/kodex/workspace" \
  -e HOME=/tmp -e OTEL_SDK_DISABLED=true -e SYNTHETIC_MODEL_KEY=fixture-only-do-not-inherit \
  "$image" runtime-workspace-executor)
ready=false
for _ in {1..50}; do
  if docker exec "$daemon" /usr/bin/test -S /run/kodex/workspace/authority/executor.sock; then ready=true; break; fi
  [[ $(docker inspect --format '{{.State.Running}}' "$daemon") == true ]] || break
  sleep 0.1
done
[[ "$ready" == true ]] || { printf 'Executor fixture did not become ready\n' >&2; docker logs "$daemon"; exit 1; }
for uid in 10002 10001; do
  docker run --rm --network none --read-only --user "$uid:29000" --cap-drop ALL \
    --security-opt no-new-privileges --pids-limit 64 --memory 128m --cpus 1 \
    --entrypoint /usr/local/bin/workspace-executor-probe \
    --mount "type=volume,src=$socket,dst=/run/kodex/workspace" "$image"
done
exit_code=$(timeout 15s docker wait "$daemon")
[[ "$exit_code" == 0 ]] || { printf 'Executor fixture did not stop cleanly\n' >&2; exit 1; }
printf 'PASS isolated PID1 executor fixture\n'
