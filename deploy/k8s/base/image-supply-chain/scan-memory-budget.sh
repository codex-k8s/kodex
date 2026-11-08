#!/bin/sh
set -eu

# Пути задаёт доверенный scan entrypoint; argv инструмента передаётся без eval.
if [ "$#" -lt 3 ]; then
  echo 'scan memory budget arguments are invalid' >&2
  exit 64
fi
budget_file=$1
cgroup_limit_file=$2
shift 2
case "$1" in syft|grype) ;; *) echo 'scan memory budget tool is invalid' >&2; exit 64 ;; esac

# Отсутствующий профиль сохраняет прежний запуск, в том числе на cgroup v1.
if [ ! -e "$budget_file" ] && [ ! -L "$budget_file" ]; then
  exec "$@"
fi

invalid_budget() {
  echo 'scan memory budget is invalid' >&2
  exit 78
}

# Не читаем неограниченный файл и не принимаем shell/env-синтаксис.
[ -f "$budget_file" ] && [ -r "$budget_file" ] || invalid_budget
budget_size=$(wc -c <"$budget_file" | tr -d ' ')
[ "$budget_size" -ge 1 ] && [ "$budget_size" -le 6 ] || invalid_budget
budget=$(cat "$budget_file")
case "$budget" in ''|0*|*[!0-9]*) invalid_budget ;; esac
[ "$budget_size" -eq "${#budget}" ] || {
  [ "$budget_size" -eq "$((${#budget} + 1))" ] || invalid_budget
  printf '%s\n' "$budget" | cmp -s - "$budget_file" || invalid_budget
}
[ "$budget" -ge 128 ] && [ "$budget" -le 65536 ] || invalid_budget

# Профиль требует конечный cgroup v2 limit и оставляет не менее 25% вне Go heap.
[ -f "$cgroup_limit_file" ] && [ -r "$cgroup_limit_file" ] || invalid_budget
limit_size=$(wc -c <"$cgroup_limit_file" | tr -d ' ')
[ "$limit_size" -ge 1 ] && [ "$limit_size" -le 15 ] || invalid_budget
limit=$(cat "$cgroup_limit_file")
case "$limit" in ''|0*|*[!0-9]*) invalid_budget ;; esac
[ "${#limit}" -le 14 ] || invalid_budget
[ "$limit_size" -eq "${#limit}" ] || {
  [ "$limit_size" -eq "$((${#limit} + 1))" ] || invalid_budget
  printf '%s\n' "$limit" | cmp -s - "$cgroup_limit_file" || invalid_budget
}
[ "$((budget * 1048576))" -le "$((limit * 3 / 4))" ] || invalid_budget

# Это мягкий предел Go runtime, не обещание отсутствия OOM и не retry инструмента.
GOMEMLIMIT="${budget}MiB" exec "$@"
