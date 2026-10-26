#!/usr/bin/env bash
# Подключение только объявляет прежнюю проверку имени registry для двух ARC скриптов.

# Принимает имя с точкой, длиной до 253 символов и строчными DNS-метками до 63.
# Сохраняет запрет окончаний .svc и .svc.cluster.local; URL, порт и wildcard
# отклоняются прежним выражением. Не разрешает адрес и не меняет переменные
# вызывающего скрипта; сохраняет прежнюю политику, в том числе числовые метки.
valid_dns_host() {
  local host=$1 label
  local -a labels=()

  [[ ${#host} -le 253 && "$host" == *.* ]] || return 1
  [[ "$host" != .* && "$host" != *. && "$host" != *..* ]] || return 1
  [[ "$host" != *.svc && "$host" != *.svc.cluster.local ]] || return 1
  IFS='.' read -r -a labels <<<"$host"
  ((${#labels[@]} >= 2)) || return 1
  for label in "${labels[@]}"; do
    [[ ${#label} -le 63 ]] || return 1
    [[ "$label" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || return 1
  done
}
