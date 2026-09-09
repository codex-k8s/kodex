#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fixture_source="$temporary_directory/source"
fixture_state="$temporary_directory/private state"
mkdir -m 0700 "$fixture_source" "$fixture_state"
prepare_output() {
  bash "$repository_root/tools/dev/prepare-smoke-output.sh" \
    --repository-root "$fixture_source" --state-directory "$1"
}
fail() { printf 'Smoke private output contract failed: %s\n' "$*" >&2; exit 1; }

first_output=$(prepare_output "$fixture_state")
[[ "$first_output" == "$fixture_state/e2e/smoke."*/artifacts ]] || fail 'unexpected output boundary'
[[ ! -e "$first_output" ]] || fail 'output child must be fresh'
[[ $(stat -c '%a' "$(dirname "$first_output")") == 700 ]] || fail 'run parent is not private'
mkdir "$first_output"
printf 'retained evidence\n' >"$first_output/retained.txt"
second_output=$(prepare_output "$fixture_state")
[[ "$first_output" != "$second_output" ]] || fail 'output reused'
[[ -f "$first_output/retained.txt" ]] || fail 'previous evidence was removed'

mkdir -m 0700 "$fixture_source/private"
if prepare_output "$fixture_source/private" >/dev/null 2>&1; then fail 'source-local state accepted'; fi
ln -s "$fixture_source/private" "$temporary_directory/source-alias"
if prepare_output "$temporary_directory/source-alias" >/dev/null 2>&1; then fail 'source symlink accepted'; fi
chmod 0755 "$fixture_state/e2e"
if prepare_output "$fixture_state" >/dev/null 2>&1; then fail 'public evidence directory accepted'; fi
[[ $(stat -c '%a' "$fixture_state/e2e") == 755 ]] || fail 'existing permissions were silently changed'
chmod 0700 "$fixture_state/e2e"
chmod 0755 "$fixture_state"
prepare_output "$fixture_state" >/dev/null
[[ $(stat -c '%a' "$fixture_state") == 755 ]] || fail 'unrelated state permissions were changed'
mkdir -m 0700 "$temporary_directory/symlink-state" "$temporary_directory/elsewhere"
ln -s "$temporary_directory/elsewhere" "$temporary_directory/symlink-state/e2e"
if prepare_output "$temporary_directory/symlink-state" >/dev/null 2>&1; then fail 'symlink evidence accepted'; fi

rg -Fq 'KODEX_E2E_PRIVATE_OUTPUT_DIR="$smoke_output_directory"' "$repository_root/dev.sh" || fail 'entrypoint output contract missing'
rg -Fq 'process.env.KODEX_E2E_PRIVATE_OUTPUT_DIR' "$repository_root/services/staff/control-center/playwright.local.config.ts" || fail 'Playwright output contract missing'
printf 'Smoke private output contract passed\n'
