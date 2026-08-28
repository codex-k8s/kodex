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

air_version=${KODEX_DEV_AIR_VERSION:-v1.63.4}
air_binary=/go/tools/air
if [ ! -x "$air_binary" ]; then
  install_lock=/go/tools/.air-install.lock
  while ! mkdir "$install_lock" 2>/dev/null; do sleep 1; done
  if [ ! -x "$air_binary" ]; then
    GOBIN=/go/tools GOWORK=off go install "github.com/air-verse/air@$air_version"
  fi
  rmdir "$install_lock"
fi

runtime_root="/tmp/kodex-dev-$name"
config="$runtime_root/air.toml"
entrypoint="\"$runtime_root/build/main\""
for argument in "$@"; do
  printf '%s' "$argument" | grep -Eq '^[A-Za-z0-9._:/=-]+$' || fail 'process argument is invalid'
  entrypoint="$entrypoint, \"$argument\""
done
mkdir -p "$runtime_root"
cat >"$config" <<EOF
root = "$module_root"
tmp_dir = "$runtime_root/build"

[build]
cmd = "CGO_ENABLED=0 GOWORK=off go build -trimpath -buildvcs=false -o $runtime_root/build/main $package"
entrypoint = [$entrypoint]
include_ext = ["go", "json", "sql", "yaml", "yml", "toml"]
exclude_dir = [".git", ".kodex-dev", "node_modules", "tmp", "vendor"]
exclude_regex = ["_test[.]go$"]
delay = 250
poll = true
poll_interval = 500
stop_on_error = true
send_interrupt = true
kill_delay = "2s"
# Локальные sidecar и service процессы могут стартовать раньше соседней
# зависимости во время одновременного apply. Air повторяет только запуск уже
# собранного бинаря; production lifecycle этим профилем не изменяется.
rerun = true
rerun_delay = 2000

[log]
time = true

[misc]
clean_on_exit = true
EOF

cd "$module_root"
exec "$air_binary" -c "$config"
