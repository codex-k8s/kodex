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
trusted_base_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
frontend_sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
runtime_contract_sha256=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
pull_host=registry.nodes.example.test
kubectl kustomize "$repository_root/deploy/k8s/overlays/staging/image-supply-chain" \
  >"$temporary_directory/supply.yaml"
for policy_name in mattercodex-image-admission-controller-jobs \
  mattercodex-image-admission-controller-workspaces; do
  POLICY_NAME="$policy_name" yq -e '
    select(.kind == "ValidatingAdmissionPolicy" and .metadata.name == strenv(POLICY_NAME)) |
    .spec.failurePolicy == "Fail"
  ' "$temporary_directory/supply.yaml" >/dev/null || {
    echo "controller admission policy is missing: $policy_name" >&2
    exit 1
  }
done
yq -e '
  select(.kind == "Role" and .metadata.name == "image-admission-controller") |
  ([.rules[].resources[]] | sort | join(",")) == "configmaps,jobs,persistentvolumeclaims"
' "$temporary_directory/supply.yaml" >/dev/null || {
  echo "image admission controller RBAC expanded beyond bounded resources" >&2
  exit 1
}
if grep -Fq 'component: kube-apiserver' "$temporary_directory/supply.yaml" ||
  ! grep -Fq 'cidr: __MATTERCODEX_KUBERNETES_API_SERVICE_CIDR__' "$temporary_directory/supply.yaml"; then
  echo "image admission controller Kubernetes API destination is not render-bound" >&2
  exit 1
fi
render_pull_host=registry-pull.invalid
render_tools_image=admission-tools.invalid/mattercodex/image-admission-tools@sha256:0000000000000000000000000000000000000000000000000000000000000000
render_admission_image=mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/image-admission@sha256:0000000000000000000000000000000000000000000000000000000000000000
[[ $(grep -F -c "common_name: $render_pull_host" "$temporary_directory/supply.yaml") -eq 2 ]]
[[ $(grep -F -c 'value: require-and-verify-client-cert' "$temporary_directory/supply.yaml") -eq 3 ]]
grep -Fq 'kubernetes.io/metadata.name: __MATTERCODEX_INGRESS_NAMESPACE__' \
  "$temporary_directory/supply.yaml"
grep -Fq 'app.kubernetes.io/name: __MATTERCODEX_INGRESS_POD_NAME__' \
  "$temporary_directory/supply.yaml"
[[ $(grep -F -c "$render_tools_image" "$temporary_directory/supply.yaml") -ge 5 ]]
for binary in registry-pull-authorizer registry-write-authorizer node-pull-bootstrap; do
  binary_images=$(MC195_BINARY="/usr/local/bin/$binary" yq eval-all '
    select(.kind == "Deployment" or .kind == "DaemonSet") |
    .spec.template.spec.containers[] | select(.command[]? == strenv(MC195_BINARY)) | .image' \
    "$temporary_directory/supply.yaml" | grep -v '^---$')
  [[ -n $binary_images ]] && ! grep -Fvxq "$render_admission_image" <<<"$binary_images" || {
    echo "$binary does not use the image that contains the compiled binary" >&2
    exit 1
  }
done
grep -Fq 'openssl x509 -in /identity/tls.crt -checkend 900' "$temporary_directory/supply.yaml"
grep -Fq 'DOCKER_CONFIG_FILE' "$temporary_directory/supply.yaml"
grep -Fq 'registry-pull.invalid/mattercodex/control-plane@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$temporary_directory/supply.yaml"
grep -Fq 'registry-pull.invalid/mattercodex/agent-runner@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$temporary_directory/supply.yaml"
push_relative_urls=$(yq eval-all 'select(.kind == "Deployment" and .metadata.name == "mattercodex-image-registry-push") |
  .spec.template.spec.containers[] | select(.name == "registry") | .env[] |
  select(.name == "REGISTRY_HTTP_RELATIVEURLS") | .value' "$temporary_directory/supply.yaml")
[[ $push_relative_urls == "true" ]] || { echo "staging registry does not return relative upload locations" >&2; exit 1; }
grep -Fq 'mattercodex-image-registry-evidence.mattercodex-system.svc.cluster.local:5007/evidence/role-image-admission' \
  "$temporary_directory/supply.yaml"
if grep -Fq 'hostNetwork: true' "$temporary_directory/supply.yaml" ||
  ! grep -Fq 'name: mattercodex-node-pull-bootstrap-exact-paths' "$temporary_directory/supply.yaml"; then
  echo "node pull bootstrap network boundary is incomplete" >&2
  exit 1
fi
node_egress_ports=$(yq eval-all 'select(.kind == "NetworkPolicy" and .metadata.name == "mattercodex-node-pull-bootstrap-exact-paths") |
  [.spec.egress[].ports[].port] | sort | join(",")' "$temporary_directory/supply.yaml")
[[ $node_egress_ports == "53,53,8200" ]] || { echo "node bootstrap received non-DNS/Vault egress" >&2; exit 1; }
grep -Fq 'REGISTRY_AUTHORIZATION_PROFILE' "$temporary_directory/supply.yaml"
grep -Fq 'mattercodex/image-registry/evidence-admission' \
  "$repository_root/tools/configure-image-supply-chain-pki.sh"
if yq eval-all 'select(.kind == "NetworkPolicy" and .metadata.name == "mattercodex-image-registry-evidence") |
  .spec.ingress[].from[].podSelector.matchExpressions[]?.values[]' "$temporary_directory/supply.yaml" |
  grep -Fqx sign; then
  echo "signer received evidence registry network authority" >&2
  exit 1
fi
[[ $(grep -F -c 'mattercodex.dev/pull-credential-generation: "0"' \
  "$temporary_directory/supply.yaml") -eq 2 ]]
[[ $(grep -F -c 'pullCredentialGeneration: "0"' \
  "$temporary_directory/supply.yaml") -eq 1 ]]
grep -Fq 'docker-content-digest:' "$repository_root/deploy/k8s/base/image-supply-chain/registry-readiness.sh"
grep -Fq 'client-cert "$(cat /identity/registry-client.crt)"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
grep -Fq 'base-registry-client.crt' "$repository_root/deploy/k8s/base/image-supply-chain/buildkitd.toml"
grep -Fq 'staging-registry-client.crt' "$repository_root/deploy/k8s/base/image-supply-chain/buildkitd.toml"
grep -Fq 'staging/readiness:rootless-probe,push=true' \
  "$repository_root/deploy/k8s/base/image-supply-chain/buildkit.yaml"
grep -Fq 'client-cert "$(cat "${certificate_file}")"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/cleanup.sh"
if rg -q 'staging-push|STAGING_DOCKER' \
  "$repository_root/deploy/k8s/base/role-image-builder"; then
  echo "builder still receives staging push authority" >&2
  exit 1
fi
if rg -q 'BuildSecretRefs|buildSecretRefs|build_secret_refs|role-image-builder-secret-resolver|input-authority' \
  "$repository_root/contracts" "$repository_root/libs/go/controlplaneapi" \
  "$repository_root/services/internal/control-plane" "$repository_root/services/external/control-api-gateway" \
  "$repository_root/services/jobs/role-image-builder" "$repository_root/deploy/k8s/base/role-image-builder"; then
  echo "removed build credential field or authority remains" >&2
  exit 1
fi
if yq eval-all 'select(.kind == "NetworkPolicy" and .metadata.name == "role-image-builder-exact-egress") |
  .spec.egress[].ports[]?.port' \
  "$repository_root/deploy/k8s/base/role-image-builder/networkpolicy.yaml" | grep -Fxq 5001; then
  echo "builder still has staging push egress" >&2
  exit 1
fi
mkdir "$temporary_directory/bin"
cat >"$temporary_directory/bin/kubectl" <<EOF
#!/bin/sh
policy_revision=\${FIXTURE_POLICY_REVISION:-7}
cat <<JSON
{"immutable":true,"metadata":{"labels":{"mattercodex.dev/owner-intent":"true"},"annotations":{"mattercodex.dev/admission-tools-sha256":"$tools_digest"}},"data":{"toolsImage":"$tools_image","admissionImage":"$admission_image","authorityImage":"registry.example.test/mattercodex/internal-rpc-authority@$authority_digest","promotionRepository":"mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003/mattercodex/roles","promotionEvidenceRepository":"mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003/mattercodex/evidence","evidenceRepository":"mattercodex-image-registry-evidence.mattercodex-system.svc.cluster.local:5007/evidence/role-image-admission","promotedPullRepository":"registry.example.test/mattercodex/roles","policyRevision":"\$policy_revision","policySHA256":"$policy_sha256","builderIdentity":"$builder_identity","buildType":"$build_type","trustedRoleBaseRepository":"registry.example.test/mattercodex/agent-runner","trustedRoleBaseDigest":"$trusted_base_digest","roleRuntimeContractRevision":"1","roleRuntimeContractSHA256":"$runtime_contract_sha256","requiredTools":"base64,cmp,cosign,grype,image-admission-bridge,jq,regctl,sha256sum,syft,wc"}}
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
policy_json=$(PATH="$temporary_directory/bin:$PATH" kubectl)
for phase in claim scan sign admit promote; do
  IMAGE_ADMISSION_POLICY_JSON="$policy_json" \
    "$repository_root/tools/render-image-admission-job.sh" staging "$build_tag" "$phase" \
    >"$temporary_directory/admission-$phase.yaml"
  [[ $(yq eval-all 'select(.kind == "Job") | .metadata.labels."mattercodex.dev/image-admission-phase"' \
    "$temporary_directory/admission-$phase.yaml" | grep -Fxc "$phase") -eq 1 ]]
done
[[ $(yq eval-all 'select(.kind == "PersistentVolumeClaim") | .metadata.name' \
  "$temporary_directory/admission-claim.yaml" | grep -c '^mc-admit-') -eq 1 ]]
for phase in scan sign admit promote; do
  [[ -z $(yq eval-all 'select(.kind == "PersistentVolumeClaim") | .metadata.name' \
    "$temporary_directory/admission-$phase.yaml" | grep -v '^---$') ]]
done
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
grep -Fq 'regctl artifact put "$@" "$evidence_tag"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
grep -Fq 'regctl artifact get "$evidence_reference" --file "$evidence_name"' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"
if rg -q -- '--slurpfile|admission\.evidence\.json' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh"; then
  echo "admission evidence still reserializes signed payloads" >&2
  exit 1
fi
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
promotion_body=$(sed -n '/^  promote)/,/^  \*)/p' \
  "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh")
grep -Fq 'restore_evidence_entries "$evidence_reference"' <<<"$promotion_body"
grep -Fq 'verify_recovered_evidence /work/evidence' <<<"$promotion_body"
if grep -Eq 'cosign\.key|sign-blob' <<<"$promotion_body"; then
  echo "promotion received evidence signing authority" >&2
  exit 1
fi
grep -Fq 'image-admission-bridge authorize-promotion' <<<"$promotion_body"
authorize_line=$(grep -n 'image-admission-bridge authorize-promotion' <<<"$promotion_body" | cut -d: -f1)
copy_line=$(grep -n 'regctl image copy "$evidence_reference"' <<<"$promotion_body" | cut -d: -f1)
[[ $authorize_line -lt $copy_line ]] || { echo "evidence was promoted before owner authorization" >&2; exit 1; }

cat >"$temporary_directory/bin/cosign" <<'EOF'
#!/bin/sh
set -eu
[ "${1:-}" = verify-blob ] || exit 1
shift
public_key=
signature=
payload=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --key) public_key=$2; shift 2 ;;
    --signature) signature=$2; shift 2 ;;
    --*) exit 1 ;;
    *) payload=$1; shift ;;
  esac
done
[ -r "$public_key" ] && [ -r "$signature" ] && [ -r "$payload" ] || exit 1
decoded_signature=$(mktemp)
trap 'rm -f -- "$decoded_signature"' EXIT HUP INT TERM
base64 -d <"$signature" >"$decoded_signature"
openssl dgst -sha256 -verify "$public_key" -signature "$decoded_signature" "$payload" >/dev/null
EOF
chmod 0555 "$temporary_directory/bin/cosign"
cat >"$temporary_directory/bin/regctl" <<'EOF'
#!/bin/sh
set -eu
[ "$#" -eq 5 ] && [ "$1" = artifact ] && [ "$2" = get ] &&
  [ "$3" = "$FIXTURE_EVIDENCE_REFERENCE" ] && [ "$4" = --file ] || exit 1
evidence_digest=$(jq -er --arg name "$5" '.layers[] |
  select(.annotations["org.opencontainers.image.title"] == $name) | .digest' \
  "$FIXTURE_EVIDENCE_MANIFEST")
cat "$FIXTURE_EVIDENCE_BLOBS/${evidence_digest#sha256:}"
EOF
chmod 0555 "$temporary_directory/bin/regctl"

evidence_entries_fixture() {
  cat <<'EOF'
image-digest.subject|application/vnd.mattercodex.image-digest.v1+text
image-digest.sig|application/vnd.dev.cosign.signature.v1+text
provenance.json|application/vnd.mattercodex.provenance-binding.v1+json
provenance.sig|application/vnd.dev.cosign.signature.v1+text
native-provenance.json|application/vnd.mattercodex.native-provenance.v1+json
native-provenance.sig|application/vnd.dev.cosign.signature.v1+text
sbom.json|application/spdx+json
sbom.sig|application/vnd.dev.cosign.signature.v1+text
vulnerability.json|application/vnd.mattercodex.vulnerability-report.v1+json
vulnerability.sig|application/vnd.dev.cosign.signature.v1+text
signature.binding.json|application/vnd.mattercodex.signature-binding.v1+json
admission.receipt.json|application/vnd.mattercodex.admission-receipt.v1+json
cosign.pub|application/vnd.dev.cosign.public-key.v1+pem
EOF
}

write_evidence_manifest_fixture() {
  evidence_directory=$1
  manifest_file=$2
  layer_file=$manifest_file.layers
  : >"$layer_file"
  while IFS='|' read -r evidence_name evidence_media_type; do
    evidence_path="$evidence_directory/$evidence_name"
    evidence_digest="sha256:$(sha256sum "$evidence_path" | awk '{print $1}')"
    evidence_size=$(wc -c <"$evidence_path" | tr -d ' ')
    jq -cn --arg media "$evidence_media_type" --arg digest "$evidence_digest" \
      --argjson size "$evidence_size" --arg title "$evidence_name" \
      '{mediaType:$media,digest:$digest,size:$size,
        annotations:{"org.opencontainers.image.title":$title}}' >>"$layer_file"
  done <<EOF
$(evidence_entries_fixture)
EOF
  jq -Ssc --arg artifact artifact-1 --arg image "$image_digest" \
    --arg policy "$policy_revision" --arg policy_sha "$policy_sha256" \
    '{schemaVersion:2,mediaType:"application/vnd.oci.image.manifest.v1+json",
      artifactType:"application/vnd.mattercodex.image-admission-evidence.v2",
      config:{mediaType:"application/vnd.mattercodex.image-admission-evidence.config.v2+json",
        digest:"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",size:2},
      layers:.,annotations:{
        "mattercodex.dev/artifact-id":$artifact,
        "mattercodex.dev/evidence-schema":"mattercodex.dev/image-admission-evidence/v2",
        "mattercodex.dev/image-digest":$image,
        "mattercodex.dev/policy-revision":$policy,
        "mattercodex.dev/policy-sha256":$policy_sha}}' "$layer_file" >"$manifest_file"
}

sign_evidence_fixture() {
  payload_file=$1
  signature_file=$2
  openssl dgst -sha256 -sign "$temporary_directory/evidence-private.pem" "$payload_file" |
    base64 | tr -d '\n' >"$signature_file"
  printf '\n' >>"$signature_file"
}

expect_evidence_failure() {
  failure_name=$1
  evidence_directory=$2
  evidence_manifest=$3
  evidence_manifest_digest="sha256:$(sha256sum "$evidence_manifest" | awk '{print $1}')"
  if PATH="$temporary_directory/bin:$PATH" \
    sh "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh" validate-evidence \
    artifact-1 "$image_digest" "$receipt_sha" "$evidence_directory" "$evidence_manifest" \
    "$evidence_manifest_digest" "$policy_revision" "$policy_sha256" ACCEPTED >/dev/null 2>&1; then
    echo "$failure_name was accepted" >&2
    exit 1
  fi
}

image_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
evidence_source="$temporary_directory/evidence-source"
evidence_blobs="$temporary_directory/evidence-blobs"
evidence_recovered="$temporary_directory/evidence-recovered"
mkdir "$evidence_source" "$evidence_blobs" "$evidence_recovered"
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out "$temporary_directory/evidence-private.pem" >/dev/null 2>&1
openssl pkey -pubout -in "$temporary_directory/evidence-private.pem" \
  -out "$evidence_source/cosign.pub" >/dev/null 2>&1
printf '%s\n' "$image_digest" >"$evidence_source/image-digest.subject"
cat >"$evidence_source/provenance.json" <<EOF
{
  "specSHA256": "1111111111111111111111111111111111111111111111111111111111111111",
  "schema": "mattercodex.dev/image-provenance-binding/v1",
  "policySHA256": "$policy_sha256",
  "manifestDigest": "$image_digest",
  "immutableBuildSHA256": "2222222222222222222222222222222222222222222222222222222222222222",
  "policyRevision": $policy_revision,
  "builderId": "$builder_identity",
  "buildType": "$build_type"
}
EOF
cat >"$evidence_source/native-provenance.json" <<'EOF'
[
  { "predicate": { "runDetails": { "builder": { "id": "fixture" } } },
    "subject": [{ "name": "role-image" }] }
]
EOF
cat >"$evidence_source/sbom.json" <<'EOF'
{
  "packages": [ { "versionInfo": "1.0", "name": "fixture" } ],
  "spdxVersion": "SPDX-2.3",
  "name": "unsorted-fixture"
}
EOF
cat >"$evidence_source/vulnerability.json" <<'EOF'
{
  "matches": [ ],
  "descriptor": { "configuration": { "fail-on-severity": "high" }, "name": "grype" }
}
EOF
for signed_name in image-digest provenance native-provenance sbom vulnerability; do
  signed_file="$evidence_source/$signed_name.json"
  [[ $signed_name == image-digest ]] && signed_file="$evidence_source/image-digest.subject"
  sign_evidence_fixture "$signed_file" "$evidence_source/$signed_name.sig"
done
signature_identity=$(sha256sum "$evidence_source/cosign.pub" | awk '{print $1}')
cat >"$evidence_source/signature.binding.json" <<EOF
{
  "verification": "cosign-key-v1", "version": "v1",
  "verdict": "ACCEPTED", "signatureIdentity": "$signature_identity",
  "policySHA256": "$policy_sha256", "policyRevision": "$policy_revision",
  "imageDigest": "$image_digest"
}
EOF
provenance_sha=$(sha256sum "$evidence_source/provenance.json" | awk '{print $1}')
sbom_sha=$(sha256sum "$evidence_source/sbom.json" | awk '{print $1}')
vulnerability_sha=$(sha256sum "$evidence_source/vulnerability.json" | awk '{print $1}')
signature_sha=$(sha256sum "$evidence_source/signature.binding.json" | awk '{print $1}')
cat >"$evidence_source/admission.receipt.json" <<EOF
{
  "verdict": "ACCEPTED",
  "version": "v1", "artifactId": "artifact-1",
  "signatureSHA256": "$signature_sha",
  "imageDigest": "$image_digest",
  "policySHA256": "$policy_sha256", "policyRevision": "$policy_revision",
  "vulnerabilityEvidenceSHA256": "$vulnerability_sha",
  "signatureIdentity": "$signature_identity",
  "specSHA256": "1111111111111111111111111111111111111111111111111111111111111111",
  "sbomSHA256": "$sbom_sha",
  "immutableBuildSHA256": "2222222222222222222222222222222222222222222222222222222222222222",
  "provenanceSHA256": "$provenance_sha"
}
EOF
receipt_sha=$(sha256sum "$evidence_source/admission.receipt.json" | awk '{print $1}')
jq -S . "$evidence_source/admission.receipt.json" >"$temporary_directory/evidence-reencoded.json"
if cmp -s "$evidence_source/admission.receipt.json" "$temporary_directory/evidence-reencoded.json"; then
  echo "byte-preservation fixture unexpectedly uses canonical JSON" >&2
  exit 1
fi

evidence_manifest="$temporary_directory/evidence-manifest.json"
write_evidence_manifest_fixture "$evidence_source" "$evidence_manifest"
while IFS=$'\t' read -r evidence_name evidence_digest; do
  cp "$evidence_source/$evidence_name" "$evidence_blobs/${evidence_digest#sha256:}"
done < <(jq -r '.layers[] | [.annotations["org.opencontainers.image.title"],.digest] | @tsv' \
  "$evidence_manifest")
evidence_manifest_digest="sha256:$(sha256sum "$evidence_manifest" | awk '{print $1}')"
evidence_reference="fixture.invalid/evidence@$evidence_manifest_digest"
FIXTURE_EVIDENCE_REFERENCE="$evidence_reference" FIXTURE_EVIDENCE_MANIFEST="$evidence_manifest" \
  FIXTURE_EVIDENCE_BLOBS="$evidence_blobs" PATH="$temporary_directory/bin:$PATH" \
  sh "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh" validate-evidence-recovery \
  artifact-1 "$image_digest" "$receipt_sha" "$evidence_reference" "$evidence_manifest" \
  "$evidence_manifest_digest" "$policy_revision" "$policy_sha256" ACCEPTED "$evidence_recovered"
while IFS='|' read -r evidence_name evidence_media_type; do
  cmp -s "$evidence_source/$evidence_name" "$evidence_recovered/$evidence_name" || {
    echo "OCI evidence byte round-trip changed $evidence_name" >&2
    exit 1
  }
done <<EOF
$(evidence_entries_fixture)
EOF
[[ $(sha256sum "$evidence_recovered/admission.receipt.json" | awk '{print $1}') == "$receipt_sha" ]]
[[ $(sha256sum "$evidence_recovered/signature.binding.json" | awk '{print $1}') == "$signature_sha" ]]

cp -a "$evidence_recovered" "$temporary_directory/evidence-byte-mutated"
printf ' ' >>"$temporary_directory/evidence-byte-mutated/sbom.json"
expect_evidence_failure "mutated OCI evidence byte" "$temporary_directory/evidence-byte-mutated" "$evidence_manifest"
jq '.layers[0].size += 1' "$evidence_manifest" >"$temporary_directory/evidence-descriptor-mismatch.json"
expect_evidence_failure "mismatched OCI evidence descriptor" "$evidence_recovered" \
  "$temporary_directory/evidence-descriptor-mismatch.json"
jq 'del(.layers[0])' "$evidence_manifest" >"$temporary_directory/evidence-missing.json"
expect_evidence_failure "OCI evidence manifest with missing layer" "$evidence_recovered" \
  "$temporary_directory/evidence-missing.json"
jq '.layers += [.layers[0]]' "$evidence_manifest" >"$temporary_directory/evidence-duplicate.json"
expect_evidence_failure "OCI evidence manifest with duplicate layer" "$evidence_recovered" \
  "$temporary_directory/evidence-duplicate.json"
jq '.layers[0].annotations["org.opencontainers.image.title"] = "foreign.json"' \
  "$evidence_manifest" >"$temporary_directory/evidence-foreign.json"
expect_evidence_failure "OCI evidence manifest with foreign layer" "$evidence_recovered" \
  "$temporary_directory/evidence-foreign.json"
cp -a "$evidence_recovered" "$temporary_directory/evidence-signature-mutated"
printf 'A' >>"$temporary_directory/evidence-signature-mutated/sbom.sig"
write_evidence_manifest_fixture "$temporary_directory/evidence-signature-mutated" \
  "$temporary_directory/evidence-signature-mutated.json"
expect_evidence_failure "mutated detached evidence signature" \
  "$temporary_directory/evidence-signature-mutated" "$temporary_directory/evidence-signature-mutated.json"

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

jq -n '{User:"10001:10001",
  Entrypoint:["/usr/local/bin/mattercodex-init","entrypoint","/usr/local/bin/matter-codex-agent-runner"],
  Cmd:["runtime-session"]}' >"$temporary_directory/runtime-config.json"
sh "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh" \
  validate-runtime-config "$temporary_directory/runtime-config.json"
jq '.Entrypoint = ["/bin/sh"]' "$temporary_directory/runtime-config.json" \
  >"$temporary_directory/unsafe-runtime-config.json"
if sh "$repository_root/deploy/k8s/base/image-supply-chain/image-admission.sh" \
  validate-runtime-config "$temporary_directory/unsafe-runtime-config.json" >/dev/null 2>&1; then
  echo "unsafe role runtime ABI was accepted" >&2
  exit 1
fi

echo "image supply-chain negative fixtures passed"
