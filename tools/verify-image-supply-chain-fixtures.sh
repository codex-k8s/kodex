#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$script_dir/.." && pwd)
policy="$repository_root/deploy/k8s/base/image-supply-chain/provenance-policy.jq"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

image_hex=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
source_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
build_tag=v20260801000000-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
subject=registry.example.test/mattercodex/control-plane
builder_identity=spiffe://mattercodex.local/ns/mattercodex-system/sa/mattercodex-role-image-builder
build_type=https://mobyproject.org/buildkit@v1
tools_digest=sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
policy_revision=7
jq -n --arg image "$image_hex" --arg source "$source_digest" \
  --arg subject "$subject" --arg build_tag "$build_tag" \
  --arg builder "$builder_identity" --arg build_type "$build_type" \
  --arg tools "$tools_digest" --arg policy "$policy_revision" '{
  _type: "https://in-toto.io/Statement/v1",
  predicateType: "https://slsa.dev/provenance/v1",
  subject: [{name: $subject, digest: {sha256: $image}}],
  predicate: {
    buildDefinition: {
      buildType: $build_type,
      externalParameters: {args: {
        "label:mattercodex.dev/source-sha256": $source,
        "label:mattercodex.dev/build-tag": $build_tag,
        "label:mattercodex.dev/admission-tools-sha256": $tools,
        "label:mattercodex.dev/admission-policy-revision": $policy
      }},
      resolvedDependencies: [{
        uri: "docker-image://docker.io/library/alpine@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
        digest: {sha256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
      }, {
        uri: ("mattercodex:source/" + $build_tag),
        digest: {sha256: ($source | sub("^sha256:"; ""))}
      }]
    },
    runDetails: {builder: {id: $builder}}
  }
}' >"$temporary_directory/valid.json"
policy_args=(--arg image "$image_hex" --arg source "$source_digest" \
  --arg subject "$subject" --arg build_tag "$build_tag" --arg tools_digest "$tools_digest" \
  --arg policy_revision "$policy_revision" --arg builder_id "$builder_identity" \
  --arg build_type "$build_type")
jq -e "${policy_args[@]}" \
  -f "$policy" "$temporary_directory/valid.json" >/dev/null

jq '.predicate.buildDefinition.externalParameters.args["label:mattercodex.dev/source-sha256"] =
  "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' \
  "$temporary_directory/valid.json" >"$temporary_directory/substituted.json"
if jq -e "${policy_args[@]}" \
  -f "$policy" "$temporary_directory/substituted.json" >/dev/null; then
  echo "substituted provenance was accepted" >&2
  exit 1
fi
jq '(.predicate.buildDefinition.resolvedDependencies[] |
  select(.uri | startswith("mattercodex:source/")) | .digest.sha256) =
  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' \
  "$temporary_directory/valid.json" >"$temporary_directory/evil-material.json"
if jq -e "${policy_args[@]}" -f "$policy" "$temporary_directory/evil-material.json" >/dev/null; then
  echo "foreign source material was accepted" >&2
  exit 1
fi

jq 'del(.subject)' "$temporary_directory/valid.json" >"$temporary_directory/no-subject.json"
if jq -e "${policy_args[@]}" \
  -f "$policy" "$temporary_directory/no-subject.json" >/dev/null; then
  echo "provenance without exact subject was accepted" >&2
  exit 1
fi
jq '.predicate.runDetails.builder.id = "spiffe://evil.invalid/builder"' \
  "$temporary_directory/valid.json" >"$temporary_directory/evil-builder.json"
if jq -e "${policy_args[@]}" -f "$policy" "$temporary_directory/evil-builder.json" >/dev/null; then
  echo "untrusted builder was accepted" >&2
  exit 1
fi
jq '.subject += [.subject[0]]' "$temporary_directory/valid.json" >"$temporary_directory/extra-subject.json"
if jq -e "${policy_args[@]}" -f "$policy" "$temporary_directory/extra-subject.json" >/dev/null; then
  echo "extra provenance subject was accepted" >&2
  exit 1
fi
jq '.predicate.buildDefinition.resolvedDependencies +=
  [.predicate.buildDefinition.resolvedDependencies[0]]' \
  "$temporary_directory/valid.json" >"$temporary_directory/duplicate-material.json"
if jq -e "${policy_args[@]}" -f "$policy" "$temporary_directory/duplicate-material.json" >/dev/null; then
  echo "duplicate resolved dependency was accepted" >&2
  exit 1
fi

control_digest=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
tools_image="registry.example.test/mattercodex/admission-tools@$tools_digest"
pull_host=registry.nodes.example.test
"$repository_root/tools/render-image-supply-chain.sh" staging \
  "$control_digest" "$pull_host" "$tools_image" 7 3 >"$temporary_directory/supply.yaml"
[[ $(grep -F -c "common_name: $pull_host" "$temporary_directory/supply.yaml") -eq 2 ]]
[[ $(grep -F -c 'value: require-and-verify-client-cert' "$temporary_directory/supply.yaml") -eq 3 ]]
[[ $(grep -F -c "$tools_image" "$temporary_directory/supply.yaml") -ge 5 ]]
grep -Fq 'openssl x509 -in /identity/tls.crt -checkend 900' "$temporary_directory/supply.yaml"
grep -Fq 'DOCKER_CONFIG_FILE' "$temporary_directory/supply.yaml"
grep -Fq "$pull_host/mattercodex/control-plane@$control_digest" "$temporary_directory/supply.yaml"
[[ $(grep -F -c 'mattercodex.dev/pull-credential-generation: "3"' \
  "$temporary_directory/supply.yaml") -eq 2 ]]
grep -Fq 'docker-content-digest:' "$repository_root/deploy/k8s/base/image-supply-chain/registry-readiness.sh"
grep -Fq 'client-cert "$(cat /identity/registry-client.crt)"' "$temporary_directory/supply.yaml"
grep -Fq 'registry-client.crt' "$repository_root/deploy/k8s/base/image-supply-chain/buildkitd.toml"
grep -Fq 'client-cert "$(cat "${certificate_file}")"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/cleanup.sh"
if "$repository_root/tools/render-image-supply-chain.sh" staging \
  "$control_digest" registry-pull.mattercodex-system.svc.cluster.local \
  "$tools_image" 7 3 >/dev/null 2>&1; then
  echo "internal Service DNS was accepted as node pull SAN" >&2
  exit 1
fi

mkdir "$temporary_directory/bin"
cat >"$temporary_directory/bin/kubectl" <<EOF
#!/bin/sh
policy_revision=\${FIXTURE_POLICY_REVISION:-7}
cat <<JSON
{"immutable":true,"metadata":{"labels":{"mattercodex.dev/owner-intent":"true"},"annotations":{"mattercodex.dev/admission-tools-sha256":"$tools_digest"}},"data":{"toolsImage":"$tools_image","policyRevision":"\$policy_revision","builderIdentity":"$builder_identity","buildType":"$build_type","scannerIdentity":"mattercodex-image-scanner","signerIdentity":"mattercodex-image-signer","admissionOwnerIdentity":"mattercodex-image-admission-owner","promotionIdentity":"mattercodex-image-promotion-writer","requiredTools":"base64,buildctl,cmp,cosign,curl,date,grype,jq,openssl,pgrep,regctl,sha256sum,syft"}}
JSON
EOF
chmod 0555 "$temporary_directory/bin/kubectl"
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  "$build_tag" \
  "$source_digest" control-plane "sha256:$image_hex" \
  >"$temporary_directory/admission.yaml"
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" production \
  "$build_tag" "$source_digest" control-plane "sha256:$image_hex" \
  >"$temporary_directory/admission-production.yaml"
[[ $(yq eval-all 'select(.kind == "Job") | .metadata.name' \
  "$temporary_directory/admission.yaml" | grep -c '^mc-admit-') -eq 4 ]]
[[ $(yq eval-all 'select(.kind == "Job") | .metadata.name' \
  "$temporary_directory/admission-production.yaml" | grep -c '^mc-admit-') -eq 4 ]]
for service_account in mattercodex-image-scanner mattercodex-image-signer \
  mattercodex-image-admission-owner mattercodex-image-promotion-writer; do
  grep -Fq "serviceAccountName: $service_account" "$temporary_directory/admission.yaml"
done
FIXTURE_POLICY_REVISION=8 PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  "$build_tag" "$source_digest" control-plane "sha256:$image_hex" \
  >"$temporary_directory/admission-policy-8.yaml"
claim_7=$(yq eval-all 'select(.kind == "PersistentVolumeClaim") | .metadata.name' \
  "$temporary_directory/admission.yaml")
claim_8=$(yq eval-all 'select(.kind == "PersistentVolumeClaim") | .metadata.name' \
  "$temporary_directory/admission-policy-8.yaml")
[[ -n $claim_7 && -n $claim_8 && $claim_7 != "$claim_8" ]] || {
  echo "policy revision reused admission evidence storage" >&2
  exit 1
}
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-build-job.sh" staging "$build_tag" source-pvc \
  "$source_digest" control-plane >"$temporary_directory/build.yaml"
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-build-job.sh" production "$build_tag" source-pvc \
  "$source_digest" control-plane >"$temporary_directory/build-production.yaml"
grep -Fq "builder-id=$builder_identity" "$temporary_directory/build.yaml"
grep -Fq "label:mattercodex.dev/admission-policy-revision=7" "$temporary_directory/build.yaml"
grep -Fq 'attestation/trusted-build/v1' "$temporary_directory/build.yaml"
grep -Fq 'mattercodex:source/' "$temporary_directory/build.yaml"
grep -Fq 'builder.key' "$temporary_directory/build.yaml"
grep -Fq 'activeDeadlineSeconds: 1800' "$temporary_directory/build-production.yaml"
grep -Fq 'verify-attestation --key /identity/builder.pub' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"

auth=$(printf 'pull-reader:current-password' | base64 | tr -d '\n')
jq -n --arg host "$pull_host" --arg auth "$auth" '{auths:{($host):{auth:$auth}}}' \
  >"$temporary_directory/dockerconfig.json"
SERVER_NAME="$pull_host" PULL_CREDENTIAL_GENERATION=3 \
  DOCKER_CONFIG_FILE="$temporary_directory/dockerconfig.json" \
  sh "$repository_root/deploy/k8s/base/image-supply-chain/registry-readiness.sh" \
  validate-docker-config >/dev/null
jq '.auths = {"registry.other.invalid": .auths["registry.nodes.example.test"]}' \
  "$temporary_directory/dockerconfig.json" >"$temporary_directory/stale-dockerconfig.json"
if SERVER_NAME="$pull_host" PULL_CREDENTIAL_GENERATION=4 \
  DOCKER_CONFIG_FILE="$temporary_directory/stale-dockerconfig.json" \
  sh "$repository_root/deploy/k8s/base/image-supply-chain/registry-readiness.sh" \
  validate-docker-config >/dev/null 2>&1; then
  echo "stale pull credential generation was accepted" >&2
  exit 1
fi
if PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  "$build_tag" \
  "$source_digest" control-plane "sha256:$image_hex" \
  attacker.invalid/tools@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  >/dev/null 2>&1; then
  echo "caller-selected admission tools image was accepted" >&2
  exit 1
fi

echo "image supply-chain negative fixtures passed"
