#!/usr/bin/env bash
# Общая входная граница трёх installer wrappers; source только объявляет функции.

# Печатает прежнюю справку с именем entrypoint, не начиная установку/readback.
kodex_user_tool_usage() {
  printf 'Usage: %s --mode apply|readback --state-directory <absolute-path>\n' "$0" >&2
}

# Возвращает mode и state_directory в одноимённые переменные caller.
# Сохраняет last-wins, help/unknown/missing-value и порядок mode→path проверок.
# fail принадлежит wrapper; caller очищает свои аргументы через set -- после
# успеха. Функция не создаёт файлов и не исполняет .env или установочные команды.
kodex_user_tool_parse() {
  mode=""
  state_directory=""
  while (($# > 0)); do
    case "$1" in
      --mode) mode=${2:-}; shift 2 ;;
      --state-directory) state_directory=${2:-}; shift 2 ;;
      --help) kodex_user_tool_usage; exit 0 ;;
      *) kodex_user_tool_usage; fail "unsupported argument: $1" ;;
    esac
  done

  case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
  [[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
    fail 'state directory is invalid'
}
