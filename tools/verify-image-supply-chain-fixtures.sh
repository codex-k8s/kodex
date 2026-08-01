#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$script_dir/.." && pwd)
policy="$repository_root/deploy/k8s/base/image-supply-chain/provenance-policy.jq"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

image_hex=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
source_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
jq -n --arg image "$image_hex" --arg source "$source_digest" '{
  _type: "https://in-toto.io/Statement/v1",
  predicateType: "https://slsa.dev/provenance/v1",
  subject: [{name: "mattercodex/control-plane", digest: {sha256: $image}}],
  predicate: {buildDefinition: {externalParameters: {sourceDigest: $source}}}
}' >"$temporary_directory/valid.json"
jq -e --arg image "$image_hex" --arg source "$source_digest" \
  -f "$policy" "$temporary_directory/valid.json" >/dev/null

jq '.predicate.buildDefinition.externalParameters.sourceDigest =
  "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' \
  "$temporary_directory/valid.json" >"$temporary_directory/substituted.json"
if jq -e --arg image "$image_hex" --arg source "$source_digest" \
  -f "$policy" "$temporary_directory/substituted.json" >/dev/null; then
  echo "substituted provenance was accepted" >&2
  exit 1
fi

jq 'del(.subject)' "$temporary_directory/valid.json" >"$temporary_directory/no-subject.json"
if jq -e --arg image "$image_hex" --arg source "$source_digest" \
  -f "$policy" "$temporary_directory/no-subject.json" >/dev/null; then
  echo "provenance without exact subject was accepted" >&2
  exit 1
fi

control_digest=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
tools_digest=sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
tools_image="registry.example.test/mattercodex/admission-tools@$tools_digest"
pull_host=registry.nodes.example.test
"$repository_root/tools/render-image-supply-chain.sh" staging \
  "$control_digest" "$pull_host" "$tools_image" 7 >"$temporary_directory/supply.yaml"
[[ $(grep -F -c "common_name: $pull_host" "$temporary_directory/supply.yaml") -eq 2 ]]
[[ $(grep -F -c 'value: require-and-verify-client-cert' "$temporary_directory/supply.yaml") -eq 3 ]]
[[ $(grep -F -c "$tools_image" "$temporary_directory/supply.yaml") -ge 5 ]]
grep -Fq 'openssl x509 -in /identity/tls.crt -checkend 900' "$temporary_directory/supply.yaml"
grep -Fq 'client-cert "$(cat /identity/registry-client.crt)"' "$temporary_directory/supply.yaml"
grep -Fq 'registry-client.crt' "$repository_root/deploy/k8s/base/image-supply-chain/buildkitd.toml"
grep -Fq 'client-cert "$(cat "${certificate_file}")"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/cleanup.sh"
if "$repository_root/tools/render-image-supply-chain.sh" staging \
  "$control_digest" registry-pull.mattercodex-system.svc.cluster.local \
  "$tools_image" 7 >/dev/null 2>&1; then
  echo "internal Service DNS was accepted as node pull SAN" >&2
  exit 1
fi

mkdir "$temporary_directory/bin"
cat >"$temporary_directory/bin/kubectl" <<EOF
#!/bin/sh
cat <<'JSON'
{"immutable":true,"metadata":{"labels":{"mattercodex.dev/owner-intent":"true"},"annotations":{"mattercodex.dev/admission-tools-sha256":"$tools_digest"}},"data":{"toolsImage":"$tools_image","policyRevision":"7","requiredTools":"base64,cmp,cosign,curl,date,grype,jq,openssl,pgrep,regctl,sha256sum,syft"}}
JSON
EOF
chmod 0555 "$temporary_directory/bin/kubectl"
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  v20260801000000-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  "$source_digest" control-plane "sha256:$image_hex" \
  >"$temporary_directory/admission.yaml"
[[ $(yq eval-all 'select(.kind == "Job") | .metadata.name' \
  "$temporary_directory/admission.yaml" | grep -c '^mc-admit-') -eq 4 ]]
for service_account in mattercodex-image-scanner mattercodex-image-signer \
  mattercodex-image-admission-owner mattercodex-image-promotion-writer; do
  grep -Fq "serviceAccountName: $service_account" "$temporary_directory/admission.yaml"
done
if PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  v20260801000000-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  "$source_digest" control-plane "sha256:$image_hex" \
  attacker.invalid/tools@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  >/dev/null 2>&1; then
  echo "caller-selected admission tools image was accepted" >&2
  exit 1
fi

echo "image supply-chain negative fixtures passed"
