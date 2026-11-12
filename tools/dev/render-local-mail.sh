#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Local mail projection failed: %s\n' "$*" >&2; exit 1; }
[[ $# == 4 ]] || fail 'expected input render, mailbox configuration, trusted resolver and output render'
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
input=$1 configuration=$2 resolver=$3 output=$4
resolver=$(realpath -e -- "$resolver") || fail 'trusted resolver file is unavailable'
for path in "$input" "$configuration" "$resolver"; do
  [[ -f "$path" && -s "$path" && ! -L "$path" ]] || fail 'input must be a regular file'
done
[[ ! -L "$output" ]] || fail 'output must not be a symlink'
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
# Producer проверяет typed source и строит CNI pins вместе с immutable policy.
yq -o=json -I=0 '.' "$input" | jq -sj '
  [.[] | select(.kind == "ConfigMap" and (.metadata.name | startswith("egress-gateway-policy-")))] |
  if length == 1 then .[0].data["policy.json"] else error("exact base policy required") end
' >"$temporary/policy.json"
revision=$(jq -er '.metadata.revision' "$temporary/policy.json")
digest=$(yq -o=json -I=0 '.' "$input" | jq -ser '
  [.[] | select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
    .spec.template.spec.containers[] | select(.name == "egress-gateway") |
    .env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST").value] |
  if length == 1 then .[0] else error("exact base policy digest required") end
')
(
  cd -- "$root/services/external/egress-gateway"
  go run ./cmd/mail-projection -configuration "$configuration" \
    -policy "$temporary/policy.json" \
    -revision "$revision" -digest "$digest" -resolv-conf "$resolver" -output "$temporary/mail"
)
source_hash=$(yq -o=json -I=0 '.' "$configuration" | jq -cS '.' | sha256sum | awk '{print $1}')
yq -o=json -I=0 '.' "$input" | jq -s --arg sourceHash "$source_hash" \
  --slurpfile cm "$temporary/mail/mail-configmap.json" \
  --slurpfile np "$temporary/mail/mail-networkpolicy.json" \
  --slurpfile patch "$temporary/mail/mail-deployment-patch.json" '
  def one($kind; $name): map(select(.kind == $kind and .metadata.name == $name)) | length == 1;
  if (one("Deployment"; "egress-gateway") and one("Deployment"; "email-bridge") and
      one("ConfigMap"; "email-bridge-runtime") and one("ConfigMap"; "kodex-dev-source-provenance")) | not
  then error("mail consumers are missing or duplicated") else . end |
  ($cm[0].data["mail-policy.json"] | fromjson) as $policy |
  $patch[0].spec.template.spec.containers[0].env[0].value as $digest |
  map(select((.kind != "ConfigMap" or .data["mail-policy.json"] == null) and
    (.kind != "NetworkPolicy" or .metadata.name != "egress-gateway-mail-destinations"))) |
  map(
    if .kind == "Deployment" and .metadata.name == "egress-gateway" then
      .spec.template.spec.containers |= map(if .name == "egress-gateway" then
        .env = ((.env // [] | map(select(.name != "EGRESS_GATEWAY_MAIL_POLICY_DIGEST"))) +
          [{name:"EGRESS_GATEWAY_MAIL_POLICY_DIGEST",value:$digest}]) else . end) |
      .spec.template.spec.volumes |= map(if .name == "mail-policy" then
        $patch[0].spec.template.spec.volumes[0] else . end)
    elif .kind == "ConfigMap" and .metadata.name == "email-bridge-runtime" then
      .data.EMAIL_BRIDGE_EGRESS_POLICY_DIGEST = $digest
    elif .kind == "ConfigMap" and .metadata.name == "kodex-dev-source-provenance" then
      .data.mailSourceSHA256 = $sourceHash |
      .data.mailConfigurationRevision = ($policy.configurationRevision | tostring) |
      .data.mailConfigurationDigest = $policy.configurationDigest |
      .data.mailPolicyDigest = $digest
    else . end |
    if .kind == "Deployment" and (.metadata.name == "egress-gateway" or .metadata.name == "email-bridge") then
      .spec.template.metadata.annotations["kodex.dev/mail-configuration-digest"] = $policy.configurationDigest |
      .spec.template.metadata.annotations["kodex.dev/mail-policy-digest"] = $digest
    else . end
  ) + [$cm[0], $np[0]] | .[]
' | yq -p=json -P >"$temporary/result.yaml"
[[ -s "$temporary/result.yaml" ]] || fail 'empty projection result'
mv -- "$temporary/result.yaml" "$output"
