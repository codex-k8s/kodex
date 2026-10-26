#!/usr/bin/env bash
# Подключение только объявляет общий загрузчик двух установщиков.

# Читает URL и SHA-256 именованного артефакта из уже выбранного lock_file.
# Сохраняет прежние jq, HTTPS/TLS, повторы curl и проверку загруженных байтов;
# имя и выходной путь передаются буквально. fail принадлежит вызывающему скрипту.
# Все промежуточные переменные локальны; установка и чтение env здесь не выполняются.
download_artifact() {
  local name=$1 output=$2 url expected_sha actual_sha
  url=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .url' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .sha256' "$lock_file")
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 15 \
    "$url" --output "$output"
  actual_sha=$(sha256sum "$output" | awk '{print $1}')
  [[ "$actual_sha" == "$expected_sha" ]] || fail "artifact digest mismatch: $name"
}
