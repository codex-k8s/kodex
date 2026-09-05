#!/usr/bin/env sh
set -eu

fail() {
  printf 'Kodex development authority socket init failed: %s\n' "$*" >&2
  exit 1
}

module_root=/workspace/services/internal/internal-rpc-authority
test -r "$module_root/go.mod" || fail 'Go module is absent'
runtime_uid=$(id -u)
case "$runtime_uid" in
  0) command -v su >/dev/null 2>&1 || fail 'su is required' ;;
  29000) test "$(id -g)" = 29000 || fail 'socket group is invalid' ;;
  *) fail 'socket identity is invalid' ;;
esac
GOTMPDIR=/go/build-cache/socket-init-tmp
HOME=/go/build-cache/socket-init-home
export GOTMPDIR HOME
mkdir -p "$GOTMPDIR" "$HOME"

cd "$module_root"
CGO_ENABLED=0 GOWORK=off go build -trimpath -buildvcs=false \
  -o /usr/local/bin/internal-rpc-authority-local-readiness \
  ./cmd/internal-rpc-authority-local-readiness
CGO_ENABLED=0 GOWORK=off go build -trimpath -buildvcs=false \
  -o /tmp/internal-rpc-authority-socket-init \
  ./cmd/internal-rpc-authority-socket-init

if test "$runtime_uid" = 29000; then
  exec /tmp/internal-rpc-authority-socket-init
fi

printf '%s\n' 'kodex-socket:x:29000:' >>/etc/group
printf '%s\n' 'kodex-socket:x:29000:29000:Kodex socket init:/tmp:/bin/sh' >>/etc/passwd
exec su -p -s /bin/sh kodex-socket -c \
  'exec /tmp/internal-rpc-authority-socket-init'
