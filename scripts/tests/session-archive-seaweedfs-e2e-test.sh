#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Session archive SeaweedFS E2E failed: %s\n' "$*" >&2
  exit 1
}

endpoint=${SESSION_ARCHIVE_E2E_ENDPOINT:-}
access_key_file=${SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE:-}
secret_key_file=${SESSION_ARCHIVE_E2E_SECRET_KEY_FILE:-}

[[ "$endpoint" =~ ^http://(127\.0\.0\.1|localhost):[1-9][0-9]{0,4}$ ]] ||
  fail 'endpoint must be an explicit loopback HTTP endpoint'
for path in "$access_key_file" "$secret_key_file"; do
  [[ "$path" == /* && -f "$path" && -s "$path" && ! -L "$path" ]] ||
    fail 'credential file must be an absolute non-symlink regular file'
done

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$repository_root/services/jobs/session-archive"
SESSION_ARCHIVE_E2E=1 \
SESSION_ARCHIVE_E2E_ENDPOINT="$endpoint" \
SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE="$access_key_file" \
SESSION_ARCHIVE_E2E_SECRET_KEY_FILE="$secret_key_file" \
  go test ./internal/archive -run '^TestSeaweedFSSnapshotRestoreDeleteE2E$' -count=1 \
  -timeout=2m
