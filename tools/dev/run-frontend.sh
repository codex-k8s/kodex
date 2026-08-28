#!/usr/bin/env sh
set -eu

fail() {
  printf 'Kodex development frontend failed: %s\n' "$*" >&2
  exit 1
}

root=/workspace/services/staff/control-center
test -r "$root/package-lock.json" || fail 'frontend lock file is absent'
cd "$root"

lock_digest=$(sha256sum package-lock.json | awk '{print $1}')
installed_digest=$(cat node_modules/.kodex-lock-digest 2>/dev/null || true)
if [ "$installed_digest" != "$lock_digest" ]; then
  npm ci --no-audit --no-fund
  printf '%s' "$lock_digest" >node_modules/.kodex-lock-digest
fi

exec npm run dev -- --host 0.0.0.0 --port 8080 --strictPort
