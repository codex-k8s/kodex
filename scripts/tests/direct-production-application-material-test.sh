#!/usr/bin/env bash
set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
classifier="$repository_root/tools/deploy/classify-direct-production-application-material.sh"
policy="$repository_root/infra/direct-production/application-material-policy.json"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

classification="$temporary_directory/classification.json"
"$classifier" --output "$classification" >/dev/null
[[ "$(stat -c '%a' "$classification")" == 600 ]] || {
  printf 'Application material classification permissions are not 0600\n' >&2
  exit 1
}

jq -e '
  .schema_version == 1 and
  .profile == "direct-production single-node prototype" and
  .namespace == "mattercodex-system" and
  (.resources | length) == 137 and
  ([.resources[] | select(.kind == "Secret")] | length) == 117 and
  ([.resources[] | select(.kind == "ConfigMap")] | length) == 20 and
  .counts == {
    cryptographically_generated:52,
    deterministically_derived:60,
    safely_reusable_from_existing_binding:2,
    truly_external_credential:23
  } and
  ([.external_bindings[].keys[]] | length) == 44 and
  (.resources | group_by([.kind,.name]) | all(length == 1))
' "$classification" >/dev/null || {
  printf 'Direct-production application material classification is incomplete\n' >&2
  exit 1
}

jq -e '
  ([.external_bindings[] | [.kind,.name]] | length) ==
    ([.external_bindings[] | [.kind,.name]] | unique | length) and
  ([.external_bindings[] as $binding |
    any(.resources[];
      .kind == $binding.kind and .name == $binding.name and
      .classification == "truly_external_credential")] | all) and
  all(.external_bindings[];
    (.keys | length) > 0 and (.keys | length) == (.keys | unique | length) and
    (.requirement | type == "string" and length > 0)) and
  all(.reusable_bindings[];
    .source_namespace == "matter-kodex-prod" and
    (.key_map | type == "object" and length > 0)) and
  ([.reusable_bindings[] as $binding |
    any(.resources[];
      .kind == $binding.target_kind and .name == $binding.target_name and
      (.classification == "safely_reusable_from_existing_binding" or
       .classification == "truly_external_credential"))] | all) and
  ([.publisher_owned_empty_resources[] as $binding |
    any(.resources[];
      .kind == $binding.kind and .name == $binding.name and
      .classification == "deterministically_derived")] | all)
' "$policy" >/dev/null || {
  printf 'Application material policy has an ambiguous binding\n' >&2
  exit 1
}

external_fixture="$temporary_directory/external.yaml"
jq -c '
  .external_bindings[] |
  if .kind == "Secret" then
    {
      apiVersion:"v1",
      kind,
      metadata:{name,namespace:"mattercodex-system"},
      data:(.keys | map({key:.,value:"Zml4dHVyZQ=="}) | from_entries)
    }
  else
    {
      apiVersion:"v1",
      kind,
      metadata:{name,namespace:"mattercodex-system"},
      data:(.keys | map({key:.,value:"fixture"}) | from_entries)
    }
  end
' "$policy" | yq -p=json -P >"$external_fixture"
"$classifier" --output "$temporary_directory/with-external.json" --external-material-file "$external_fixture" >/dev/null

cp "$external_fixture" "$temporary_directory/missing-key.yaml"
yq -i '
  with(select(.kind == "Secret" and
    .metadata.name == "automation-scheduler-application-grant");
    del(.data."application-grant.jws"))
' "$temporary_directory/missing-key.yaml"
if "$classifier" --output "$temporary_directory/rejected.json" --external-material-file "$temporary_directory/missing-key.yaml" >/dev/null 2>&1; then
  printf 'Incomplete external material was accepted\n' >&2
  exit 1
fi

cp "$external_fixture" "$temporary_directory/insecure.yaml"
chmod 0644 "$temporary_directory/insecure.yaml"
if "$classifier" --output "$temporary_directory/insecure-output.json" --external-material-file "$temporary_directory/insecure.yaml" >/dev/null 2>&1; then
  printf 'Insecure external material permissions were accepted\n' >&2
  exit 1
fi

if jq -r '.. | strings' "$policy" |
  grep -Eiq '(BEGIN [A-Z ]*PRIVATE KEY|password=|token=|postgres(ql)?://[^[:space:]]+@)'; then
  printf 'Application material policy contains a credential value\n' >&2
  exit 1
fi

printf 'Direct-production application material classification checks completed\n'
