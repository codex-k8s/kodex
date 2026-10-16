#!/bin/sh
# Только два marker файла внутри проверенного server container. Исходные
# K3s manifests и данные приложений worker никогда не меняет.
set +x
set -eu
umask 077
native_action=${1:-} native_component=${2:-} native_plan=${3:-} native_intent=${4:-}
native_source_hash=${5:-} native_source_metadata=${6:-} native_marker_metadata=${7:-}
fail() { printf 'Native marker transition failed\n' >&2; exit 1; }
hash_valid() { test "${#1}" -eq 64 && ! printf '%s' "$1" | LC_ALL=C grep -q '[^a-f0-9]'; }
for native_hash in "$native_plan" "$native_intent" "$native_source_hash"; do hash_valid "$native_hash" || fail; done
test "$(id -u)" = 0 || fail
case "$native_action" in install|inspect|finalize|remove) ;; *) fail ;; esac
native_root=/var/lib/rancher/k3s/server/manifests
case "$native_component" in
  coredns) native_source=$native_root/coredns.yaml ;;
  metrics-server) native_source=$native_root/metrics-server/metrics-server-deployment.yaml ;;
  *) fail ;;
esac
native_parent=$(dirname "$native_source")
native_marker=$native_source.skip
native_staging=$native_parent/.kodex-native-$native_component-$native_plan.stage.skip
native_lock=$native_root/.kodex-stable-native-lock
test -d "$native_root" && test ! -L "$native_root" && test -d "$native_parent" && test ! -L "$native_parent" || fail
native_metadata() { stat -c '%u:%g:%a:%h:%d:%i:%s' "$1"; }
native_hash_file() { sha256sum "$1" | cut -d ' ' -f 1; }
source_valid() {
  test -f "$native_source" && test ! -L "$native_source" &&
    test "$(stat -c '%u:%h' "$native_source")" = 0:1 &&
    test "$(stat -c '%u:%g:%a:%h' "$native_source")" = "$(printf '%s' "$native_source_metadata" | cut -d : -f 1-4)" &&
    test "$(native_hash_file "$native_source")" = "$native_source_hash"
}
marker_payload() { printf 'kodex.stable-native-marker/v1\n%s\n%s\n%s\n' "$native_component" "$native_plan" "$native_intent"; }
native_marker_hash=$(marker_payload | sha256sum | cut -d ' ' -f 1)
marker_valid() {
  test -f "$native_marker" && test ! -L "$native_marker" &&
    test "$(stat -c '%u:%g:%a:%h' "$native_marker")" = 0:0:600:1 &&
    test "$(native_hash_file "$native_marker")" = "$native_marker_hash"
}
linked_valid() {
  test -f "$native_marker" && test ! -L "$native_marker" && test -f "$native_staging" && test ! -L "$native_staging" &&
    test "$(stat -c '%u:%g:%a:%h' "$native_marker")" = 0:0:600:2 &&
    test "$(stat -c '%d:%i' "$native_marker")" = "$(stat -c '%d:%i' "$native_staging")" &&
    test "$(native_hash_file "$native_marker")" = "$native_marker_hash"
}
source_valid || fail
if test "$native_action" = inspect; then
  test ! -e "$native_lock" && test ! -L "$native_lock" || fail
  if test ! -e "$native_marker" && test ! -L "$native_marker"; then printf 'ABSENT\n'; exit 0; fi
  if linked_valid; then
    if test -n "$native_marker_metadata"; then test "$(native_metadata "$native_marker")" = "$native_marker_metadata" || fail; fi
    printf 'LINKED|'; native_metadata "$native_marker"; exit 0
  fi
  marker_valid || fail
  if test -n "$native_marker_metadata"; then test "$(native_metadata "$native_marker")" = "$native_marker_metadata" || fail; fi
  native_metadata "$native_marker"
  exit 0
fi
mkdir -m 0700 "$native_lock" || fail
native_lock_identity=$(stat -c '%d:%i:%u' "$native_lock")
cleanup() {
  if test -d "$native_lock" && test ! -L "$native_lock" &&
    test "$(stat -c '%d:%i:%u' "$native_lock")" = "$native_lock_identity"; then rmdir "$native_lock"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
source_valid || fail
if test "$native_action" = install; then
  test "$(native_metadata "$native_source")" = "$native_source_metadata" || fail
  test ! -e "$native_marker" && test ! -L "$native_marker" || fail
  # Сначала durable staging. Файл с .skip не является новым K3s manifest.
  # Прежний неизвестный install никогда не переигрывается по тому же имени.
  test ! -e "$native_staging" && test ! -L "$native_staging" || fail
  (set -C; marker_payload >"$native_staging") || fail
  test "$(native_hash_file "$native_staging")" = "$native_marker_hash" || fail
  sync -f "$native_parent"
  source_valid && test "$(native_metadata "$native_source")" = "$native_source_metadata" || fail
  # link() публикует полностью записанный marker без перезаписи. Crash между
  # link/unlink оставляет exact двухссылочный receipt для inspect/finalize.
  ln "$native_staging" "$native_marker" || fail
  sync -f "$native_parent"
  linked_valid || fail
  rm "$native_staging"
  sync -f "$native_parent"
  marker_valid || fail
  native_metadata "$native_marker"
  exit 0
fi
if test "$native_action" = finalize; then
  linked_valid && test -n "$native_marker_metadata" && test "$(native_metadata "$native_marker")" = "$native_marker_metadata" || fail
  rm "$native_staging"
  sync -f "$native_parent"
  marker_valid || fail
  native_metadata "$native_marker"
  exit 0
fi
test -n "$native_marker_metadata" && marker_valid && test "$(native_metadata "$native_marker")" = "$native_marker_metadata" || fail
# Оригинальный Deployment обязан быть восстановлен вызывающим orchestrator
# до remove. Под lock удаляется только exact marker receipt, не manifest.
rm "$native_marker"
sync -f "$native_parent"
test ! -e "$native_marker" && test ! -L "$native_marker" || fail
printf 'REMOVED\n'
