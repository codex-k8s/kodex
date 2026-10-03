#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Keycloak group reconcile test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/tools/deploy/configure-keycloak.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
group_state="$temporary_directory/groups.json"
operation_log="$temporary_directory/operations.log"
printf '[]\n' >"$group_state"
: >"$operation_log"
# The sourced production functions resolve this global at runtime.
# shellcheck disable=SC2034
realm=fixture

keycloak_request() {
  local operation=${1:-} resource=${2:-} assignment group_name
  shift 2
  case "$operation:$resource" in
    get:groups)
      cat "$group_state"
      ;;
    create:groups)
      group_name=""
      while (($# > 0)); do
        case "$1" in
          -r) shift 2 ;;
          -s)
            assignment=${2:-}
            shift 2
            [[ "$assignment" != name=* ]] || group_name=${assignment#name=}
            ;;
          *) return 1 ;;
        esac
      done
      [[ -n "$group_name" ]] || return 1
      jq -cS --arg id "group-$(($(jq 'length' "$group_state") + 1))" \
        --arg name "$group_name" \
        '. + [{id:$id,name:$name,path:("/" + $name)}]' \
        "$group_state" >"$group_state.next"
      mv -- "$group_state.next" "$group_state"
      printf 'create %s\n' "$group_name" >>"$operation_log"
      ;;
    *) return 1 ;;
  esac
}

extract_function() {
  local function_name=$1
  awk -v signature="${function_name}() {" '
    $0 == signature { capture = 1 }
    capture { print }
    capture && /^}$/ { exit }
  ' "$bootstrap"
}

bash -n "$bootstrap"
# shellcheck disable=SC1090
source <(extract_function find_group_id)
# shellcheck disable=SC1090
source <(extract_function reconcile_group)

for group_name in kodex-admins kodex-owners kodex-monitoring kodex-developers; do
  reconcile_group "$group_name"
done
for group_name in kodex-admins kodex-owners kodex-monitoring kodex-developers; do
  reconcile_group "$group_name"
done

[[ "$(jq 'length' "$group_state")" == 4 ]] || fail 'repeated reconcile duplicated groups'
[[ "$(wc -l <"$operation_log")" == 4 ]] || fail 'repeated reconcile performed extra mutations'
jq -e '
  ([.[].name] | sort) ==
    ["kodex-admins", "kodex-developers", "kodex-monitoring", "kodex-owners"] and
  all(.[]; .path == ("/" + .name))
' "$group_state" >/dev/null || fail 'canonical root groups are invalid'

jq -cS '. + [{id:"duplicate",name:"kodex-admins",path:"/kodex-admins"}]' \
  "$group_state" >"$group_state.next"
mv -- "$group_state.next" "$group_state"
operations_before_duplicate=$(wc -l <"$operation_log")
if (reconcile_group kodex-admins) >"$temporary_directory/duplicate.out" \
  2>"$temporary_directory/duplicate.err"; then
  fail 'duplicate group names were accepted'
fi
grep -Fq 'Keycloak group is duplicated: kodex-admins' \
  "$temporary_directory/duplicate.err" || fail 'duplicate group failure is not explicit'
[[ "$(wc -l <"$operation_log")" == "$operations_before_duplicate" ]] ||
  fail 'duplicate group state was mutated before fail-closed rejection'

printf 'Keycloak group reconcile test completed\n'
