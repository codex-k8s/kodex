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
for command_name in grep install readlink tail; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
tool_bin="$state_directory/tools/bin"
render_root="$state_directory/tools/render"
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

for command_name in kubectl yq; do
  [[ -x "$tool_bin/$command_name" ]] || fail "$command_name is absent"
  resolved=$(readlink -f "$tool_bin/$command_name")
  case "$resolved" in "$render_root"/*) ;; *) fail "$command_name resolves outside the private tool root" ;; esac
done
"$tool_bin/kubectl" version --client -o json >/dev/null || fail 'kubectl readback failed'
"$tool_bin/yq" --version | grep -Fq 'version v4.53.6' || fail 'yq readback failed'

printf 'Kodex user render tools ready: %s\n' "$tool_bin"
