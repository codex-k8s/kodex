#!/bin/sh
# Выполняется только внутри exact k3d node; payload приходит через stdin.
set +x
set -eu
umask 077
mirror_mode=${1:-} mirror_intent=${2:-} registry_before=${3:-} registry_after=${4:-}
config_before=${5:-} config_after=${6:-} registry_owner=${7:-} config_owner=${8:-}
fail() { printf 'Node registry transition failed\n' >&2; exit 1; }
hash_valid() { test "${#1}" -eq 64 && ! printf '%s' "$1" | LC_ALL=C grep -q '[^a-f0-9]'; }
if ! hash_valid "$mirror_intent" || ! hash_valid "$registry_before" || ! hash_valid "$registry_after" || ! hash_valid "$config_after"; then fail; fi
test "$config_before" = ABSENT || hash_valid "$config_before" || fail
for mirror_owner in "$registry_owner" "$config_owner"; do
  case "$mirror_owner" in ''|*[!0-9]*) fail ;; esac
done
test "$(id -u)" = 0 || fail
case "$mirror_mode" in install|rollback|verify-backup) ;; *) fail ;; esac
mirror_directory=/etc/rancher/k3s
registry_path=$mirror_directory/registries.yaml
config_path=$mirror_directory/config.yaml
backup_directory=$mirror_directory/.kodex-stable-registry-$mirror_intent
lock_directory=$mirror_directory/.kodex-stable-registry-lock
test -d "$mirror_directory" && test ! -L "$mirror_directory" || fail

private_file() {
  test -f "$1" && test ! -L "$1" && test "$(stat -c '%a:%h' "$1")" = 600:1
}
file_hash() { sha256sum "$1" | cut -d ' ' -f 1; }
check_file() { private_file "$1" && test "$(file_hash "$1")" = "$2"; }
current_config_hash() {
  if test ! -e "$config_path" && test ! -L "$config_path"; then printf ABSENT;
  else private_file "$config_path" || fail; file_hash "$config_path"; fi
}
check_backups() {
  test -d "$backup_directory" && test ! -L "$backup_directory" &&
    test "$(stat -c '%u:%a' "$backup_directory")" = 0:700 || fail
  check_file "$backup_directory/registries.before" "$registry_before" || fail
  check_file "$backup_directory/intent" "$mirror_intent_file_hash" || fail
  test "$(cat "$backup_directory/intent")" = "$mirror_intent" || fail
  if test "$config_before" = ABSENT; then
    private_file "$backup_directory/config.absent" && test ! -s "$backup_directory/config.absent" || fail
  else check_file "$backup_directory/config.before" "$config_before" || fail; fi
}
mirror_intent_file_hash=$(printf '%s\n' "$mirror_intent" | sha256sum | cut -d ' ' -f 1)

if test "$mirror_mode" != verify-backup; then
  mkdir -m 0700 "$lock_directory" || fail
  mirror_lock_identity=$(stat -c '%d:%i:%u' "$lock_directory")
  registry_temporary='' config_temporary=''
  cleanup() {
    test -z "$registry_temporary" || rm -f "$registry_temporary"
    test -z "$config_temporary" || rm -f "$config_temporary"
    if test -d "$lock_directory" && test ! -L "$lock_directory" &&
      test "$(stat -c '%d:%i:%u' "$lock_directory")" = "$mirror_lock_identity"; then rmdir "$lock_directory"; fi
  }
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM
else
  test ! -e "$lock_directory" && test ! -L "$lock_directory" || fail
fi

if test "$mirror_mode" = install; then
  check_file "$registry_path" "$registry_before" && test "$(stat -c '%u' "$registry_path")" = "$registry_owner" || fail
  test "$(current_config_hash)" = "$config_before" || fail
  if test "$config_before" != ABSENT; then test "$(stat -c '%u' "$config_path")" = "$config_owner" || fail; fi
  # Exclusive mkdir — неизвестный прежний install не переигрывается.
  mkdir -m 0700 "$backup_directory" || fail
  cp -p "$registry_path" "$backup_directory/registries.before"
  if test "$config_before" = ABSENT; then : >"$backup_directory/config.absent";
  else cp -p "$config_path" "$backup_directory/config.before"; fi
  printf '%s\n' "$mirror_intent" >"$backup_directory/intent"
  sync
  IFS= read -r mirror_registry_payload || fail
  IFS= read -r mirror_config_payload || fail
  registry_temporary=$(mktemp "$mirror_directory/.kodex-registry.XXXXXXXX")
  config_temporary=$(mktemp "$mirror_directory/.kodex-config.XXXXXXXX")
  printf '%s\n' "$mirror_registry_payload" >"$registry_temporary"
  printf '%s\n' "$mirror_config_payload" >"$config_temporary"
  unset mirror_registry_payload mirror_config_payload
  if ! check_file "$registry_temporary" "$registry_after" || ! check_file "$config_temporary" "$config_after"; then fail; fi
  chown "$registry_owner:0" "$registry_temporary"
  chown "$config_owner:0" "$config_temporary"
  # Повторный CAS после durable backup и получения полного payload.
  check_file "$registry_path" "$registry_before" && test "$(current_config_hash)" = "$config_before" || fail
  mv -f "$registry_temporary" "$registry_path"
  mv -f "$config_temporary" "$config_path"
  sync
  if ! check_file "$registry_path" "$registry_after" || ! check_file "$config_path" "$config_after"; then fail; fi
  printf 'FILES_INSTALLED\n'
  exit 0
fi

check_backups
private_file "$registry_path" || fail
mirror_actual_registry=$(file_hash "$registry_path")
mirror_actual_config=$(current_config_hash)
test "$mirror_actual_registry" = "$registry_before" || test "$mirror_actual_registry" = "$registry_after" || fail
test "$mirror_actual_config" = "$config_before" || test "$mirror_actual_config" = "$config_after" || fail
if test "$mirror_mode" = verify-backup; then printf 'BACKUP_VERIFIED\n'; exit 0; fi

registry_temporary=$(mktemp "$mirror_directory/.kodex-registry.XXXXXXXX")
config_temporary=$(mktemp "$mirror_directory/.kodex-config.XXXXXXXX")
cp -p "$backup_directory/registries.before" "$registry_temporary"
check_file "$registry_temporary" "$registry_before" || fail
if test "$config_before" != ABSENT; then
  cp -p "$backup_directory/config.before" "$config_temporary"
  check_file "$config_temporary" "$config_before" || fail
fi
test "$(file_hash "$registry_path")" = "$mirror_actual_registry" && test "$(current_config_hash)" = "$mirror_actual_config" || fail
mv -f "$registry_temporary" "$registry_path"
if test "$config_before" = ABSENT; then rm -f "$config_path";
else mv -f "$config_temporary" "$config_path"; fi
sync
printf 'FILES_RESTORED\n'
