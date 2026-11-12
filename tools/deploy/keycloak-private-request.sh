#!/bin/sh
# Выполняется внутри Pod. Секреты поступают только через stdin; аргументы содержат
# режим и публичные параметры kcadm. Каждый вызов удаляет собственный auth cache.
set -eu
umask 077
auth_kind=$1
input_kind=$2
shift 2
case "$auth_kind" in client|user) ;; *) exit 64 ;; esac
case "$input_kind" in none|json|password) ;; *) exit 64 ;; esac
IFS= read -r identity
IFS= read -r credential
private_directory=$(mktemp -d /tmp/kodex-kcadm.XXXXXXXXXX)
trap 'rm -rf -- "$private_directory"' EXIT
trap 'exit 143' TERM
trap 'exit 130' INT
config=$private_directory/config
command=/opt/keycloak/bin/kcadm.sh
if [ "$auth_kind" = client ]; then
  KC_CLI_CLIENT_SECRET="$credential" "$command" config credentials --config "$config" \
    --server http://127.0.0.1:8080 --realm master --client "$identity" >/dev/null 2>&1
else
  KC_CLI_PASSWORD="$credential" "$command" config credentials --config "$config" \
    --server http://127.0.0.1:8080 --realm master --user "$identity" >/dev/null 2>&1
fi
unset credential
case "$input_kind" in
  none) "$command" "$@" --config "$config" ;;
  json)
    cat >"$private_directory/input.json"
    "$command" "$@" --config "$config" -f "$private_directory/input.json"
    ;;
  password)
    IFS= read -r new_password
    KC_CLI_PASSWORD="$new_password" "$command" "$@" --config "$config"
    unset new_password
    ;;
esac
