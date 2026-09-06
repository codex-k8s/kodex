#!/usr/bin/env sh
set -eu

fail() {
  printf 'Kodex development frontend failed: %s\n' "$*" >&2
  exit 1
}

root=/workspace/services/staff/control-center
test -r "$root/package-lock.json" || fail 'frontend lock file is absent'
cd "$root"

identity=$(sh /workspace/tools/dev/frontend-cache-identity.sh) || fail 'runtime identity is unavailable'
installed_identity=$(cat node_modules/.kodex-cache-identity 2>/dev/null || true)
test "$installed_identity" = "$identity" || fail 'frontend cache is absent or stale; run trusted host render'
test -r node_modules/vite/bin/vite.js || fail 'frontend cache is incomplete'

exec node node_modules/vite/bin/vite.js --configLoader runner --host 0.0.0.0 --port 8080 --strictPort
