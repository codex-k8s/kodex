#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local Go cache contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
renderer="$repository_root/tools/dev/render-local.sh"
hot_reload="$repository_root/tools/dev/run-go-hot-reload.sh"
go_command="$repository_root/tools/dev/run-go-command.sh"

grep -Fq 'go_module_cache="$cache_root/go-mod-v2"' "$renderer" ||
  fail 'versioned shared module cache is absent'
grep -Fq 'go -C "$source_root/$module" mod download' "$renderer" ||
  fail 'host-side module cache prime is absent'
grep -Fq 'go install "$air_module@$air_version"' "$renderer" ||
  fail 'locked Air installation is absent'
grep -Fq '{"name":"GOMODCACHE","value":"/go/pkg/mod"}' "$renderer" ||
  fail 'workloads do not use the shared module cache'
if grep -Fq '"/go/pkg/mod/" + strenv(CACHE_KEY)' "$renderer"; then
  fail 'per-container module cache fragmentation is present'
fi
grep -Fq 'umask 0000' "$hot_reload" || fail 'hot-reload cache umask is absent'
grep -Fq 'umask 0000' "$go_command" || fail 'Go command cache umask is absent'
grep -Fq 'root = "$repository_root"' "$hot_reload" ||
  fail 'hot reload does not observe the repository dependency graph'
grep -Fq 'include_dir = ["$module", "libs/go"]' "$hot_reload" ||
  fail 'hot reload does not observe shared Go libraries'
grep -Fq 'cd $module_root && CGO_ENABLED=0' "$hot_reload" ||
  fail 'hot reload no longer builds from the selected module'

printf 'Kodex local Go cache contract test passed\n'
