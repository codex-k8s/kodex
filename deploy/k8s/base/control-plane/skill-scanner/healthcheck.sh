#!/usr/bin/env sh
set -eu

# Ping проверяет daemon, дата — закреплённую readonly базу того же контейнера.
config=${1:-/etc/kodex-skill-scanner/clamd.conf}
clamdscan --config-file="$config" --ping=1 >/dev/null 2>&1
version=$(clamd --version)
stamp=${version#*/}
stamp=${stamp#*/}
case "$version" in
  'ClamAV '*/*/*) ;;
  *) echo 'Skill scanner database version is unavailable' >&2; exit 1 ;;
esac
database_time=$(date -u -D '%a %b %e %H:%M:%S %Y' -d "$stamp" +%s)
now=$(date -u +%s)
age=$((now - database_time))
[ "$age" -le 604800 ] && [ "$age" -ge -86400 ] || {
  echo 'Skill scanner database freshness check failed' >&2
  exit 1
}
