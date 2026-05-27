#!/usr/bin/env bash
set -euo pipefail

cat >&2 <<'MSG'
add_allowed_user.sh is deprecated.
Use one of the following instead:
1) access-manager administrative command/API once the owner-side surface is enabled
2) a migration or seed path owned by access-manager for first bootstrap environments
MSG
exit 1
