#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf 'Smoke output preparation failed: %s\n' "$*" >&2
  exit 1
}

repository_root=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --repository-root) repository_root=${2:?}; shift 2 ;;
    --state-directory) state_directory=${2:?}; shift 2 ;;
    --help)
      printf '%s\n' 'Usage: prepare-smoke-output.sh --repository-root <path> --state-directory <private-path-outside-source>'
      exit 0
      ;;
    *) fail 'unsupported argument' ;;
  esac
done

[[ -n "$repository_root" && -d "$repository_root" ]] || fail 'repository root is required'
[[ -n "$state_directory" && -d "$state_directory" ]] || fail 'existing private state directory is required'
repository_root=$(realpath -e -- "$repository_root")
state_directory=$(realpath -e -- "$state_directory")
case "$state_directory/" in
  "$repository_root/"*) fail 'state directory must be outside application source' ;;
esac
evidence_root="$state_directory/e2e"
[[ ! -L "$evidence_root" ]] || fail 'evidence directory must not be a symbolic link'
if [[ ! -e "$evidence_root" ]]; then
  mkdir -m 0700 -- "$evidence_root"
fi
[[ -d "$evidence_root" && $(stat -c '%u' -- "$evidence_root") == "$(id -u)" &&
   $(stat -c '%a' -- "$evidence_root") == 700 ]] || fail 'evidence directory must be operator-owned with mode 0700'

# Playwright очищает outputDir перед запуском; родитель уникален и остаётся закрытым.
run_directory=$(mktemp -d "$evidence_root/smoke.XXXXXXXX")
printf '%s/artifacts\n' "$run_directory"
