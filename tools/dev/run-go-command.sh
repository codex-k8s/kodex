#!/usr/bin/env sh
set -eu

fail() {
  printf 'Kodex development Go command failed: %s\n' "$*" >&2
  exit 1
}

module=${1:-}
package=${2:-}
shift 2 || true
case "$module" in
  services/*) ;;
  *) fail 'module path is invalid' ;;
esac
case "$package" in
  ./cmd/*) ;;
  *) fail 'command package is invalid' ;;
esac
module_root="/workspace/$module"
test -r "$module_root/go.mod" || fail 'Go module is absent'
runtime_uid=$(id -u)
GOTMPDIR="${GOTMPDIR:-/tmp/kodex-go}-$runtime_uid"
HOME="${HOME:-/tmp/kodex-home}-$runtime_uid"
export GOTMPDIR HOME
mkdir -p "$GOTMPDIR" "$HOME"
cd "$module_root"
exec go run -trimpath "$package" "$@"
