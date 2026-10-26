#!/usr/bin/env bash
# Общие операции двух точек входа настройки node registry. Подключение только
# объявляет функции; контекст, изменения узлов и контрольное чтение остаются
# в вызывающих скриптах, которые используют set -euo pipefail.

# Разбирает прежние четыре CLI-флага в mode/context/material_directory/promoted_pull_host.
# Сохраняет значения по умолчанию, приоритет последнего флага и прежние usage/fail
# при помощи, неизвестном флаге или пропущенном значении. Затем скрипт вызывает set --.
kodex_node_registry_parse() {
  mode=""
  context=""
  material_directory=""
  promoted_pull_host=""
  while (($# > 0)); do
    case "$1" in
      --mode) mode=${2:-}; shift 2 ;;
      --context) context=${2:-}; shift 2 ;;
      --material-directory) material_directory=${2:-}; shift 2 ;;
      --promoted-pull-host) promoted_pull_host=${2:-}; shift 2 ;;
      --help) usage; exit 0 ;;
      *) usage; fail "unsupported argument: $1" ;;
    esac
  done
}

# Читает пять файлов node-pull из уже проверенного material_directory.
# Заполняет прежние переменные путей, input_file/input_mode и username/password
# вызывающего скрипта. Проверяет непустые обычные файлы без символических ссылок,
# права, CR/LF и длину. Перед чтением снимает export с прежних шести имён.
# Подстановка команды по-прежнему удаляет завершающие LF; значения не печатаются.
kodex_node_registry_read_material() {
  username_file="$material_directory/node-pull/username"
  password_file="$material_directory/node-pull/password"
  ca_file="$material_directory/node-pull/ca.crt"
  certificate_file="$material_directory/node-pull/client.crt"
  private_key_file="$material_directory/node-pull/client.key"
  for input_file in "$username_file" "$password_file" "$ca_file" "$certificate_file" "$private_key_file"; do
    [[ -f "$input_file" && -s "$input_file" && ! -L "$input_file" ]] ||
      fail 'node pull material is incomplete'
    input_mode=$(stat -c '%a' "$input_file")
    (((8#$input_mode & 8#077) == 0)) || fail 'node pull material permissions are too broad'
  done

  # Не наследуем атрибут export одноимённых переменных вызывающей стороны.
  export -n username password actual expected actual_host expected_host
  username=$(<"$username_file")
  password=$(<"$password_file")
  [[ -n "$username" && "$username" != *$'\n'* && "$username" != *$'\r'* ]] ||
    fail 'node pull username is invalid'
  [[ ${#password} -ge 32 && "$password" != *$'\n'* && "$password" != *$'\r'* ]] ||
    fail 'node pull password is invalid'
}

# Объединяет единственное JSON-значение existing_json_file с настройками точного
# promoted_pull_host по прежней программе jq. Читает проверенные временные
# username/password и прежние system_ca/system_certificate/system_private_key.
# Сохраняет прочие узлы и поля; целевой узел получает HTTPS и прежние auth/TLS.
# Использует прежние выходные пути; к узлам не обращается, секреты не выводит.
kodex_node_registry_render() {
  jq -cn --slurpfile existing "$existing_json_file" --arg host "$promoted_pull_host" \
    --rawfile username "$temporary_directory/username" --rawfile password "$temporary_directory/password" --arg ca "$system_ca" \
    --arg certificate "$system_certificate" --arg private_key "$system_private_key" '
    (if ($existing | length) == 1 then $existing[0]
     else error("registry configuration must contain one JSON value") end) |
    .mirrors = ((.mirrors // {}) + {($host):{endpoint:[("https://" + $host)]}}) |
    .configs = ((.configs // {}) + {($host):{
      auth:{username:$username,password:$password},
      tls:{ca_file:$ca,cert_file:$certificate,key_file:$private_key}
    }})
  ' >"$expected_json_file"
  yq -P <"$expected_json_file" >"$temporary_directory/registries.yaml"
  chmod 0600 "$temporary_directory/registries.yaml"
}
