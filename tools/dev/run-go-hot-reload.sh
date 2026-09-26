#!/usr/bin/env sh
set -eu
umask 0000

fail() {
  printf 'Kodex development hot reload failed: %s\n' "$*" >&2
  exit 1
}

module=${1:-}
package=${2:-}
name=${3:-}
shift 3 || true
case "$module" in
  services/*) ;;
  *) fail 'module path is invalid' ;;
esac
case "$package" in
  ./cmd/*) ;;
  *) fail 'command package is invalid' ;;
esac
printf '%s' "$name" | grep -Eq '^[a-z0-9][a-z0-9-]*$' || fail 'process name is invalid'

repository_root=/workspace
module_root="$repository_root/$module"
test -r "$module_root/go.mod" || fail 'Go module is absent'

gomodcache=${GOMODCACHE:-$(go env GOMODCACHE)}
gopath=${GOPATH:-$(go env GOPATH)}
gocache=${GOCACHE:-/go/build-cache/$name}
gotmpdir=${GOTMPDIR:-$gocache/tmp}
home=${HOME:-$gocache/home}
for writable_directory in \
  "$gocache" \
  "$gotmpdir" \
  "$home"; do
  mkdir -p -- "$writable_directory" || fail "cannot create writable Go path: $writable_directory"
  test -w "$writable_directory" || fail "Go path is not writable: $writable_directory"
done
for readonly_directory in "$gomodcache" "$gopath/pkg/sumdb/sum.golang.org" /go/tools; do
  test -r "$readonly_directory" || fail "read-only Go path is unavailable: $readonly_directory"
  test ! -w "$readonly_directory" || fail "shared Go path must be read-only: $readonly_directory"
done

air_version=${KODEX_DEV_AIR_VERSION:-v1.67.4}
air_sha256=${KODEX_DEV_AIR_SHA256:-}
air_binary="/go/tools/air-$air_version"
# Существующие Pod прежнего render сохраняют старый бинарь до своего rollout.
if [ ! -x "$air_binary" ] && [ "$air_version" = v1.63.4 ]; then
  air_binary=/go/tools/air
fi
printf '%s' "$air_sha256" | grep -Eq '^[a-f0-9]{64}$' ||
  fail 'Air executable digest is invalid'
actual_air_sha256=$(sha256sum "$air_binary" | awk '{print $1}') ||
  fail 'Air executable digest readback failed'
[ "$actual_air_sha256" = "$air_sha256" ] || fail 'Air executable digest differs from the rendered contract'
air_is_usable() {
  [ -x "$air_binary" ] && "$air_binary" -v 2>/dev/null | grep -Fq "$air_version"
}
air_is_usable || fail 'Air executable is unavailable'

# /tmp может быть отдельным небольшим scratch tmpfs сервиса. Исполняемый
# Air binary хранится только в выделенном writable build cache этого workload.
runtime_root="/go/build-cache/runtime-$name"
mkdir -p -- "$runtime_root" || fail 'cannot create writable Air runtime path'
test -w "$runtime_root" || fail 'Air runtime path is not writable'
chmod 0700 -- "$runtime_root" || fail 'cannot secure Air runtime path'
config="$runtime_root/air.toml"
kill_delay=$(sh "$repository_root/tools/dev/go-shutdown-budget.sh" "$name")
entrypoint="\"$runtime_root/build/main\""
for argument in "$@"; do
  printf '%s' "$argument" | grep -Eq '^[A-Za-z0-9._:/=-]+$' || fail 'process argument is invalid'
  entrypoint="$entrypoint, \"$argument\""
done
mkdir -p "$runtime_root"
cat >"$config" <<EOF
root = "$repository_root"
tmp_dir = "$runtime_root/build"

[build]
cmd = "cd $module_root && CGO_ENABLED=0 GOWORK=off go build -trimpath -buildvcs=false -o $runtime_root/build/main $package"
entrypoint = [$entrypoint]
include_ext = ["go", "json", "sql", "yaml", "yml", "toml"]
include_dir = ["$module", "libs/go"]
exclude_dir = [".git", ".kodex-dev", "node_modules", "tmp", "vendor"]
exclude_regex = ["_test[.]go$"]
delay = 250
poll = true
poll_interval = 500
stop_on_error = true
send_interrupt = true
kill_delay = "$kill_delay"
# Повторный запуск упавшего процесса принадлежит liveness probe Kubernetes.
# Air с rerun=true может параллельно породить новые процессы до остановки
# прежнего и вызвать гонки bootstrap и занятие gRPC-порта.
rerun = false

[log]
time = true

[misc]
clean_on_exit = true
EOF

cd "$repository_root"
umask 0077
exec "$air_binary" -c "$config"
