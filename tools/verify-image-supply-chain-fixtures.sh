#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$script_dir/.." && pwd)
policy="$repository_root/deploy/k8s/base/image-supply-chain/provenance-policy.jq"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

image_hex=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
base_hex=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
frontend_hex=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
source_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
build_tag=v20260801000000-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
subject=registry.example.test/mattercodex/control-plane
builder_identity=spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder
build_type=https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md
tools_digest=sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
policy_revision=7
jq -n --arg image "$image_hex" --arg base "$base_hex" --arg frontend "$frontend_hex" \
  --arg subject "$subject" \
  --arg builder "$builder_identity" --arg build_type "$build_type" \
  --arg tools "$tools_digest" --arg policy "$policy_revision" '{
  _type: "https://in-toto.io/Statement/v1",
  predicateType: "https://slsa.dev/provenance/v1",
  subject: [{name: $subject, digest: {sha256: $image}}],
  predicate: {
    buildDefinition: {
      buildType: $build_type,
      resolvedDependencies: [{
        uri: "docker-image://docker.io/library/alpine@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
        digest: {sha256: $base}
      }, {
        uri: "docker-image://registry.example.test/mattercodex/dockerfile@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
        digest: {sha256: $frontend}
      }]
    },
    runDetails: {builder: {id: $builder}}
  }
}' >"$temporary_directory/valid.json"
policy_args=(--arg image "$image_hex" --arg base "$base_hex" --arg frontend "$frontend_hex" \
  --arg builder_id "$builder_identity" --arg build_type "$build_type")
jq -e "${policy_args[@]}" \
  -f "$policy" "$temporary_directory/valid.json" >/dev/null

jq '.predicate.buildDefinition.resolvedDependencies[0].digest.sha256 = "invalid"' \
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
authority_digest=sha256:abababababababababababababababababababababababababababababababab
tools_image="registry.example.test/mattercodex/admission-tools@$tools_digest"
admission_image="registry.example.test/mattercodex/image-admission@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
policy_sha256=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
pull_host=registry.nodes.example.test
"$repository_root/tools/render-image-supply-chain.sh" staging \
  "$control_digest" "$authority_digest" "$pull_host" "$tools_image" "$admission_image" 7 "$policy_sha256" 3 >"$temporary_directory/supply.yaml"
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
grep -Fq 'base-registry-client.crt' "$repository_root/deploy/k8s/base/image-supply-chain/buildkitd.toml"
grep -Fq 'staging-registry-client.crt' "$repository_root/deploy/k8s/base/image-supply-chain/buildkitd.toml"
grep -Fq 'client-cert "$(cat "${certificate_file}")"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/cleanup.sh"
if "$repository_root/tools/render-image-supply-chain.sh" staging \
  "$control_digest" "$authority_digest" registry-pull.mattercodex-system.svc.cluster.local \
  "$tools_image" "$admission_image" 7 "$policy_sha256" 3 >/dev/null 2>&1; then
  echo "internal Service DNS was accepted as node pull SAN" >&2
  exit 1
fi

mkdir "$temporary_directory/bin"
cat >"$temporary_directory/bin/kubectl" <<EOF
#!/bin/sh
policy_revision=\${FIXTURE_POLICY_REVISION:-7}
cat <<JSON
{"immutable":true,"metadata":{"labels":{"mattercodex.dev/owner-intent":"true"},"annotations":{"mattercodex.dev/admission-tools-sha256":"$tools_digest"}},"data":{"toolsImage":"$tools_image","admissionImage":"$admission_image","authorityImage":"registry.example.test/mattercodex/internal-rpc-authority@$authority_digest","promotionRepository":"mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003/mattercodex/roles","promotedPullRepository":"registry.example.test/mattercodex/roles","policyRevision":"\$policy_revision","policySHA256":"$policy_sha256","builderIdentity":"$builder_identity","buildType":"$build_type","requiredTools":"base64,cmp,cosign,grype,image-admission-bridge,jq,regctl,sha256sum,syft"}}
JSON
EOF
chmod 0555 "$temporary_directory/bin/kubectl"
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  "$build_tag" \
  >"$temporary_directory/admission.yaml"
PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" production \
  "$build_tag" \
  >"$temporary_directory/admission-production.yaml"
[[ $(yq eval-all 'select(.kind == "Job") | .metadata.name' \
  "$temporary_directory/admission.yaml" | grep -c '^mc-admit-') -eq 5 ]]
[[ $(yq eval-all 'select(.kind == "Job") | .metadata.name' \
  "$temporary_directory/admission-production.yaml" | grep -c '^mc-admit-') -eq 5 ]]
for service_account in mattercodex-image-scanner mattercodex-image-signer \
  image-admission image-promotion; do
  grep -Fq "serviceAccountName: $service_account" "$temporary_directory/admission.yaml"
done
FIXTURE_POLICY_REVISION=8 PATH="$temporary_directory/bin:$PATH" \
  "$repository_root/tools/render-image-admission-job.sh" staging \
  "$build_tag" \
  >"$temporary_directory/admission-policy-8.yaml"
claim_7=$(yq eval-all 'select(.kind == "PersistentVolumeClaim") | .metadata.name' \
  "$temporary_directory/admission.yaml")
claim_8=$(yq eval-all 'select(.kind == "PersistentVolumeClaim") | .metadata.name' \
  "$temporary_directory/admission-policy-8.yaml")
[[ -n $claim_7 && -n $claim_8 && $claim_7 != "$claim_8" ]] || {
  echo "policy revision reused admission evidence storage" >&2
  exit 1
}
grep -Fq 'serviceAccountName: role-image-builder' \
  "$repository_root/deploy/k8s/base/role-image-builder/deployment.yaml"
grep -Fq 'image-admission-bridge claim' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
grep -Fq 'IMAGE_OWNER_PROMOTION_READBACK_SHA256_FILE' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
grep -Fq 'IMAGE_OWNER_ADMISSION_RECEIPT_OCI_MANIFEST_DIGEST_FILE' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
grep -Fq 'signatureSHA256:$signature_sha' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
if grep -Fq 'issuedAt' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"; then
  echo "admission receipt contains non-deterministic issue time" >&2
  exit 1
fi
grep -Fq 'load_promotion_claim' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
promotion_uses_emptydir=$(yq eval-all 'select(.kind == "Job" and .metadata.labels."mattercodex.dev/image-admission-phase" == "promote") |
  .spec.template.spec.volumes[] | select(.name == "work") | .emptyDir != null' "$temporary_directory/admission.yaml")
[[ $promotion_uses_emptydir == "true" ]] || {
  echo "promotion still shares admission PVC" >&2
  exit 1
}
if sed -n '/^  promote)/,/^  \*)/p' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh" |
  grep -Fq 'load_owner_claim'; then
  echo "promotion still depends on admission PVC claim" >&2
  exit 1
fi

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
  attacker.invalid/tools@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  >/dev/null 2>&1; then
  echo "caller-selected admission tools image was accepted" >&2
  exit 1
fi

echo "image supply-chain negative fixtures passed"
