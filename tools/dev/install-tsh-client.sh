#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Teleport client installation failed: %s\n' "$*" >&2
  exit 1
}

mode=${1:-}
case "$mode" in apply|readback) ;; *) fail 'usage: install-tsh-client.sh apply|readback' ;; esac
((EUID != 0)) || fail 'Teleport client must be installed for an unprivileged user'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$repository_root/tools/install/components.lock.json"
install_root="$HOME/.local/lib/kodex-teleport"
binary="$install_root/tsh"
wrapper="$HOME/.local/bin/tsh-kodex"
temporary_directory=""
temporary_wrapper=""
cleanup() {
  [[ -z "$temporary_directory" ]] || rm -rf -- "$temporary_directory"
  [[ -z "$temporary_wrapper" ]] || rm -f -- "$temporary_wrapper"
}
trap cleanup EXIT
temporary_directory=$(mktemp -d)
version=$(jq -er '.artifacts[] | select(.name == "teleport-client") | .version' "$lock_file") ||
  fail 'Teleport client version lock is absent'
url=$(jq -er '.artifacts[] | select(.name == "teleport-client") | .url' "$lock_file") ||
  fail 'Teleport client URL lock is absent'
sha256=$(jq -er '.artifacts[] | select(.name == "teleport-client") | .sha256' "$lock_file") ||
  fail 'Teleport client digest lock is absent'
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$url" == https://cdn.teleport.dev/* &&
  "$sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'Teleport client lock is invalid'

render_wrapper() {
  cat <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
real_home=$(getent passwd "$(id -u)" | cut -d: -f6)
export HOME="${KODEX_TSH_HOME:-$real_home/.tsh-kodex-home}"
exec "$real_home/.local/lib/kodex-teleport/tsh" "$@"
EOF
}

if [[ "$mode" == apply ]]; then
  archive="$temporary_directory/teleport-client.tar.gz"
  curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error "$url" --output "$archive"
  printf '%s  %s\n' "$sha256" "$archive" | sha256sum --check --status ||
    fail 'Teleport client archive digest mismatch'
  tar -xzf "$archive" -C "$temporary_directory" teleport/tsh
  install -d -m 0755 "$install_root" "$HOME/.local/bin"
  install -m 0755 "$temporary_directory/teleport/tsh" "$binary"
  wrapper_candidate="$temporary_directory/tsh-kodex"
  render_wrapper >"$wrapper_candidate"
  install -m 0755 "$wrapper_candidate" "$wrapper"
fi

[[ -x "$binary" && ! -L "$binary" && -x "$wrapper" && ! -L "$wrapper" ]] ||
  fail 'Teleport client or isolated wrapper is absent'
temporary_wrapper=$(mktemp)
render_wrapper >"$temporary_wrapper"
cmp -s "$temporary_wrapper" "$wrapper" || fail 'Teleport wrapper differs from repository contract'
install -d -m 0700 "$temporary_directory/home"
HOME="$temporary_directory/home" "$binary" version |
  grep -Eq "Teleport v?$version([[:space:]]|$)" ||
  fail 'Teleport client version differs from repository lock'
printf 'Kodex Teleport client installation completed: %s\n' "$mode"
