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
grep -Fq 'chmod -R a-w "$go_module_cache" "$go_sumdb_cache" "$cache_root/go-tools"' \
  "$renderer" || fail 'shared Go material is not sealed read-only after host priming'
grep -Fq '{"name":"GOMODCACHE","value":"/go/pkg/mod"}' "$renderer" ||
  fail 'workloads do not use the shared module cache'
grep -Fq '{"name":"dev-go-mod","mountPath":"/go/pkg/mod","readOnly":true}' "$renderer" ||
  fail 'shared module cache is not mounted read-only'
grep -Fq 'go_build_cache="$cache_root/go-build-v2"' "$renderer" ||
  fail 'versioned Go build cache root is absent'
grep -Fq 'build_cache_path="$go_build_cache/$cache_key"' "$renderer" ||
  fail 'workload-specific writable build cache is absent'
if grep -Fq '"/go/pkg/mod/" + strenv(CACHE_KEY)' "$renderer"; then
  fail 'per-container module cache fragmentation is present'
fi
grep -Fq 'umask 0000' "$hot_reload" || fail 'hot-reload cache umask is absent'
grep -Fq 'test ! -w "$readonly_directory"' "$hot_reload" ||
  fail 'hot reload does not reject writable shared Go material'
grep -Fq 'umask 0000' "$go_command" || fail 'Go command cache umask is absent'
grep -Fq 'root = "$repository_root"' "$hot_reload" ||
  fail 'hot reload does not observe the repository dependency graph'
grep -Fq 'include_dir = ["$module", "libs/go"]' "$hot_reload" ||
  fail 'hot reload does not observe shared Go libraries'
grep -Fq 'cd $module_root && CGO_ENABLED=0' "$hot_reload" ||
  fail 'hot reload no longer builds from the selected module'

printf 'Kodex local Go cache contract test passed\n'
bash "$repository_root/scripts/tests/local-email-entrypoint-contract-test.sh"
