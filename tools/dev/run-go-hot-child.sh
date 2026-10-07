#!/usr/bin/env sh
set -eu

fail() {
  printf 'Kodex development child supervision failed: %s\n' "$1" >&2
  exit 1
}

[ "$#" -ge 4 ] || fail 'supervisor arguments are incomplete'
supervisor_pid=$1
supervisor_started=$2
supervisor_executable=$3
shift 3
case "$supervisor_pid:$supervisor_started" in
  *[!0-9:]*|:*|*:) fail 'supervisor identity is invalid' ;;
esac
[ "$supervisor_pid" -gt 0 ] && [ "$supervisor_started" -gt 0 ] || fail 'supervisor identity is invalid'
case "$supervisor_executable:$1" in
  /*:/*) ;;
  *) fail 'executable paths are invalid' ;;
esac
[ -x "$supervisor_executable" ] && [ -x "$1" ] || fail 'executable is unavailable'

process_started() {
  awk '{ sub(/^.*\) /, ""); print $20 }' "/proc/$1/stat" 2>/dev/null
}

supervisor_matches() {
  [ "$(process_started "$supervisor_pid")" = "$supervisor_started" ] || return 1
  [ "$(readlink -f "/proc/$supervisor_pid/exe" 2>/dev/null)" = \
    "$(readlink -f "$supervisor_executable" 2>/dev/null)" ] || return 1
  ancestor_pid=$$
  ancestor_depth=0
  while [ "$ancestor_depth" -lt 8 ]; do
    ancestor_pid=$(awk '{ sub(/^.*\) /, ""); print $2 }' "/proc/$ancestor_pid/stat" 2>/dev/null) || return 1
    [ "$ancestor_pid" = "$supervisor_pid" ] && return 0
    case "$ancestor_pid" in
      ''|0|1|*[!0-9]*) return 1 ;;
    esac
    ancestor_depth=$((ancestor_depth + 1))
  done
  return 1
}

supervisor_matches || fail 'supervisor identity does not match'
interrupted=false
service_pid=''
# Обработчик вызывается trap, а не прямым вызовом из основного потока.
# shellcheck disable=SC2329
forward_signal() {
  interrupted=true
  if [ -n "$service_pid" ]; then
    kill -"$1" "$service_pid" 2>/dev/null || true
  fi
}
trap 'forward_signal INT' INT
trap 'forward_signal TERM' TERM

# exec сохраняет argv и runtime identity сервиса. Отдельный процесс нужен
# только для wait/join: supervisor не запускает retry и не меняет readiness.
(exec "$@") &
service_pid=$!
service_status=0
if wait "$service_pid"; then
  service_status=0
else
  service_status=$?
fi
# Сигнал может прервать wait раньше выхода сервиса. Повторный wait обязательно
# присоединяет тот же процесс; kill delay по-прежнему принадлежит Air.
while kill -0 "$service_pid" 2>/dev/null; do
  if wait "$service_pid"; then
    service_status=0
  else
    service_status=$?
  fi
done
if [ "$interrupted" = false ] && [ "$service_status" -ne 0 ]; then
  printf 'Kodex development child exited unexpectedly\n' >&2
  # PID, поколение процесса, executable и ancestor проверяются снова перед
  # сигналом. Поэтому старый child не останавливает заменивший его supervisor.
  supervisor_matches || fail 'supervisor identity no longer matches'
  kill -TERM "$supervisor_pid" 2>/dev/null || fail 'supervisor termination failed'
fi
exit "$service_status"
