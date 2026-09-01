#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Keycloak protocol mapper reconcile test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/tools/deploy/configure-keycloak.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
mapper_state="$temporary_directory/mappers.json"
operation_log="$temporary_directory/operations.log"
printf '[]\n' >"$mapper_state"
: >"$operation_log"

keycloak_request() {
  local operation=${1:-} resource=${2:-} assignment name protocol mapper_type config mapper_id
  shift 2
  case "$operation" in
    get)
      [[ "$resource" == 'clients/fixture-client/protocol-mappers/models' ]] || return 1
      cat "$mapper_state"
      ;;
    create|update)
      name=""
      protocol=""
      mapper_type=""
      config=""
      while (($# > 0)); do
        case "$1" in
          -r) shift 2 ;;
          -s)
            assignment=${2:-}
            shift 2
            case "$assignment" in
              name=*) name=${assignment#name=} ;;
              protocol=*) protocol=${assignment#protocol=} ;;
              protocolMapper=*) mapper_type=${assignment#protocolMapper=} ;;
              config=*) config=${assignment#config=} ;;
            esac
            ;;
          *) return 1 ;;
        esac
      done
      [[ -n "$name" && "$protocol" == openid-connect && -n "$mapper_type" ]] || return 1
      config=$(jq -ceS 'if type == "object" then . else error("invalid config") end' <<<"$config") || return 1
      if [[ "$operation" == create ]]; then
        [[ "$resource" == 'clients/fixture-client/protocol-mappers/models' ]] || return 1
        mapper_id="fixture-mapper-$(($(jq 'length' "$mapper_state") + 1))"
        jq -cS --arg id "$mapper_id" --arg name "$name" --arg protocol "$protocol" \
          --arg mapper_type "$mapper_type" --argjson config "$config" \
          '. + [{id:$id,name:$name,protocol:$protocol,protocolMapper:$mapper_type,config:$config}]' \
          "$mapper_state" >"$mapper_state.next"
      else
        mapper_id=${resource##*/}
        [[ "$resource" == "clients/fixture-client/protocol-mappers/models/$mapper_id" ]] || return 1
        jq -ceS --arg id "$mapper_id" --arg name "$name" --arg protocol "$protocol" \
          --arg mapper_type "$mapper_type" --argjson config "$config" '
            if ([.[] | select(.id == $id)] | length) != 1 then error("mapper id mismatch")
            else map(if .id == $id then {
              id:$id,name:$name,protocol:$protocol,protocolMapper:$mapper_type,config:$config
            } else . end) end
          ' "$mapper_state" >"$mapper_state.next"
      fi
      mv -- "$mapper_state.next" "$mapper_state"
      printf '%s %s\n' "$operation" "$mapper_id" >>"$operation_log"
      ;;
    delete)
      printf 'delete %s\n' "$resource" >>"$operation_log"
      return 1
      ;;
    *) return 1 ;;
  esac
}

extract_function() {
  local function_name=$1
  awk -v signature="^${function_name}\\(\\) \\{$" '
    $0 ~ signature { capture = 1 }
    capture { print }
    capture && /^}$/ { exit }
  ' "$bootstrap"
}

bash -n "$bootstrap"
# shellcheck disable=SC1090
source <(extract_function require_mapper_exact)
# shellcheck disable=SC1090
source <(extract_function reconcile_mapper)

expected_config=$(jq -cn '{
  "claim.name":"groups",
  "full.path":"false",
  "access.token.claim":"true",
  "id.token.claim":"true"
}')
reconcile_mapper fixture-client fixture-mapper oidc-group-membership-mapper \
  "$expected_config" fixture-realm
first_mapper_id=$(jq -er '.[0].id' "$mapper_state")
reconcile_mapper fixture-client fixture-mapper oidc-group-membership-mapper \
  "$expected_config" fixture-realm

[[ "$(grep -c '^create ' "$operation_log" || true)" == 1 ]] ||
  fail 'repeated reconcile created a new mapper'
[[ "$(grep -c '^update ' "$operation_log" || true)" == 1 ]] ||
  fail 'existing mapper was not updated exactly once'
[[ "$(grep -c '^delete ' "$operation_log" || true)" == 0 ]] ||
  fail 'repeated reconcile deleted a mapper'
jq -e --arg id "$first_mapper_id" --argjson config "$expected_config" '
  length == 1 and .[0] == {
    id:$id,
    name:"fixture-mapper",
    protocol:"openid-connect",
    protocolMapper:"oidc-group-membership-mapper",
    config:$config
  }
' "$mapper_state" >/dev/null || fail 'repeated reconcile changed mapper ID or exact config'
require_mapper_exact "$(<"$mapper_state")" fixture-mapper \
  oidc-group-membership-mapper "$expected_config" ||
  fail 'exact mapper readback rejected the stable mapper'

jq -cS '. + [.[0] | .id = "fixture-mapper-duplicate"]' "$mapper_state" >"$mapper_state.next"
mv -- "$mapper_state.next" "$mapper_state"
operations_before_duplicate=$(wc -l <"$operation_log")
if (reconcile_mapper fixture-client fixture-mapper oidc-group-membership-mapper \
  "$expected_config" fixture-realm) >"$temporary_directory/duplicate.out" \
  2>"$temporary_directory/duplicate.err"; then
  fail 'duplicate mapper names were accepted'
fi
grep -Fq 'Keycloak protocol mapper is duplicated: fixture-mapper' \
  "$temporary_directory/duplicate.err" || fail 'duplicate mapper failure is not explicit'
[[ "$(wc -l <"$operation_log")" == "$operations_before_duplicate" ]] ||
  fail 'duplicate mapper state was mutated before fail-closed rejection'
if require_mapper_exact "$(<"$mapper_state")" fixture-mapper \
  oidc-group-membership-mapper "$expected_config"; then
  fail 'exact mapper readback accepted duplicate names'
fi

if rg -q 'replace_mapper' "$bootstrap"; then
  fail 'legacy create-only mapper helper remains active'
fi
[[ "$(rg -c '^[[:space:]]*reconcile_mapper ' "$bootstrap")" == 7 ]] ||
  fail 'not every canonical mapper uses stable reconcile'

printf 'Keycloak protocol mapper reconcile test completed\n'
