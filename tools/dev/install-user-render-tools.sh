#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex user render tools installation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --mode apply|readback --state-directory <absolute-path>\n' "$0" >&2
}

mode=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory is invalid'
for command_name in awk curl grep install ln mktemp mv python3 readlink sha256sum tail tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$repository_root/tools/install/components.lock.json"
tool_bin="$state_directory/tools/bin"
render_root="$state_directory/tools/render"
toolchain_root="$state_directory/tools/toolchains"
if [[ "$mode" == apply && ( ! -x "$tool_bin/yq" || ! -x "$tool_bin/kubectl" ) ]]; then
  install -d -m 0700 "$tool_bin" "$render_root"
  github_path="$render_root/github-path"
  : >"$github_path"
  chmod 0600 "$github_path"
  RUNNER_TEMP="$render_root" GITHUB_PATH="$github_path" \
    "$repository_root/tools/release/install-render-tools.sh" >/dev/null
  installed_directory=$(tail -n 1 "$github_path")
  case "$installed_directory" in "$render_root"/*) ;; *) fail 'render tool path is unsafe' ;; esac
  for command_name in kubectl yq; do
    [[ -x "$installed_directory/$command_name" ]] || fail "installed $command_name is absent"
    ln -sfn "$installed_directory/$command_name" "$tool_bin/$command_name"
  done
fi

read -r go_version go_url go_sha256 < <(python3 - "$lock_file" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    artifacts = json.load(source).get("artifacts", [])
matches = [item for item in artifacts if item.get("name") == "go"]
if len(matches) != 1:
    raise SystemExit(1)
item = matches[0]
print(item.get("version", ""), item.get("url", ""), item.get("sha256", ""))
PY
) || fail 'Go toolchain lock is absent'
[[ "$go_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ &&
  "$go_url" == "https://go.dev/dl/go${go_version}.linux-amd64.tar.gz" &&
  "$go_sha256" =~ ^[a-f0-9]{64}$ ]] || fail 'Go toolchain lock is invalid'
go_install="$toolchain_root/go-$go_version"
if [[ "$mode" == apply && ! -x "$go_install/bin/go" ]]; then
  [[ ! -e "$go_install" ]] || fail 'partial Go toolchain installation exists'
  install -d -m 0700 "$toolchain_root"
  temporary_directory=$(mktemp -d "$toolchain_root/.install.XXXXXX")
  trap 'rm -rf -- "$temporary_directory"' EXIT
  archive="$temporary_directory/go.tar.gz"
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 15 \
    "$go_url" --output "$archive"
  printf '%s  %s\n' "$go_sha256" "$archive" | sha256sum --check --status ||
    fail 'Go toolchain archive digest mismatch'
  tar -tzf "$archive" | awk '
    BEGIN { found = 0 }
    /^\// || /(^|\/)\.\.(\/|$)/ || $0 !~ /^go\// { exit 1 }
    { found = 1 }
    END { if (!found) exit 1 }
  ' || fail 'Go toolchain archive contains an unsafe path'
  tar -xzf "$archive" -C "$temporary_directory"
  [[ -x "$temporary_directory/go/bin/go" && -x "$temporary_directory/go/bin/gofmt" ]] ||
    fail 'Go toolchain archive is incomplete'
  mv -- "$temporary_directory/go" "$go_install"
fi
for command_name in go gofmt; do
  [[ -x "$go_install/bin/$command_name" && ! -L "$go_install/bin/$command_name" ]] ||
    fail "$command_name is absent from the private Go toolchain"
  ln -sfn "$go_install/bin/$command_name" "$tool_bin/$command_name"
  resolved=$(readlink -f "$tool_bin/$command_name")
  [[ "$resolved" == "$go_install/bin/$command_name" ]] ||
    fail "$command_name resolves outside the private Go toolchain"
done
[[ "$("$tool_bin/go" env GOVERSION)" == "go$go_version" ]] ||
  fail 'Go toolchain version differs from repository lock'

for command_name in kubectl yq; do
  [[ -x "$tool_bin/$command_name" ]] || fail "$command_name is absent"
  resolved=$(readlink -f "$tool_bin/$command_name")
  case "$resolved" in "$render_root"/*) ;; *) fail "$command_name resolves outside the private tool root" ;; esac
done
"$tool_bin/kubectl" version --client -o json >/dev/null || fail 'kubectl readback failed'
"$tool_bin/yq" --version | grep -Fq 'version v4.53.6' || fail 'yq readback failed'

printf 'Kodex user render tools ready: %s\n' "$tool_bin"
