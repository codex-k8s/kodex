#!/bin/sh
set -eu

fail() {
  echo "image admission failed: $1" >&2
  exit 1
}

wait_for_file() {
  file=$1
  remaining=120
  while [ ! -f "/work/$file" ] && [ "$remaining" -gt 0 ]; do
    sleep 10
    remaining=$((remaining - 1))
  done
  [ -f "/work/$file" ] || fail "predecessor evidence timeout"
}

write_marker() {
  printf '%s\n' "$ADMISSION_RUN_ID" >"/work/$1"
}

wait_for_marker() {
  wait_for_file "$1"
  [ "$(cat "/work/$1")" = "$ADMISSION_RUN_ID" ] || fail "stale predecessor evidence"
}

require_policy() {
  echo "$POLICY_REVISION" | grep -Eq '^[1-9][0-9]*$' || fail "invalid policy revision"
  echo "$POLICY_SHA256" | grep -Eq '^[a-f0-9]{64}$' || fail "invalid policy digest"
  echo "$ADMISSION_RUN_ID" | grep -Eq '^v[0-9]{14}-[a-f0-9]{40}$' || fail "invalid admission run ID"
  echo "$ADMISSION_TOOLS_IMAGE" | grep -Eq '^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$' ||
    fail "admission tools image is not immutable"
  echo "$ADMISSION_IMAGE" | grep -Eq '^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$' ||
    fail "admission image is not immutable"
  echo "$PROMOTION_REPOSITORY" | grep -Eq '^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$' ||
    fail "promotion repository is invalid"
  echo "$PROMOTED_PULL_REPOSITORY" | grep -Eq '^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$' ||
    fail "promoted pull repository is invalid"
  [ "${PROMOTION_REPOSITORY#*/}" = "${PROMOTED_PULL_REPOSITORY#*/}" ] ||
    fail "promotion and pull repository paths differ"
  [ "$EXPECTED_BUILDER_ID" = "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder" ] ||
    fail "untrusted builder identity"
  [ "$EXPECTED_BUILD_TYPE" = "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md" ] ||
    fail "untrusted build type"
  echo "$ROLE_RUNTIME_CONTRACT_REVISION" | grep -Eq '^[1-9][0-9]*$' || fail "invalid runtime contract revision"
  echo "$ROLE_RUNTIME_CONTRACT_SHA256" | grep -Eq '^[a-f0-9]{64}$' || fail "invalid runtime contract digest"
  echo "$TRUSTED_ROLE_BASE_REPOSITORY" | grep -Eq '^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$' ||
    fail "trusted role base repository is invalid"
  echo "$TRUSTED_ROLE_BASE_DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || fail "trusted role base digest is invalid"
  for tool in base64 cmp cosign grype image-admission-bridge jq regctl sha256sum syft; do
    command -v "$tool" >/dev/null || fail "admission image is incomplete"
  done
}

verify_runtime_config() {
  config_file=$1
  jq -e '
    .User == "10001:10001" and
    .Entrypoint == ["/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/matter-codex-agent-runner"] and
    .Cmd == ["runtime-session"]
  ' "$config_file" >/dev/null || fail "role runtime ABI mismatch"
}

load_owner_claim() {
  wait_for_file owner-claim.json
  jq -e --argjson policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" '
    . as $claim |
    (.artifactId | type == "string" and length > 0) and
    (.version | type == "number" and . > 0) and
    (.fence | type == "number" and . > 0) and
    (.claimToken | type == "string" and length > 0) and
    (.recipeId | type == "string" and length > 0) and
    (.recipeVersion | type == "number" and . > 0) and
    (.recipeGeneration | type == "number" and . > 0) and
    (.specSHA256 | test("^[a-f0-9]{64}$")) and
    (.buildId | type == "string" and length > 0) and
    (.buildVersion | type == "number" and . > 0) and
    (.buildAttempt | type == "number" and . > 0) and
    (.stagingReference | test("^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*@sha256:[a-f0-9]{64}$")) and
    (.manifestDigest | test("^sha256:[a-f0-9]{64}$")) and
    (.immutableBuildSHA256 | test("^[a-f0-9]{64}$")) and
    (.provenanceSHA256 | test("^[a-f0-9]{64}$")) and
    (.baseImageDigest | test("^sha256:[a-f0-9]{64}$")) and
    (.sourceSHA256 | test("^[a-f0-9]{64}$")) and
    (.contextSHA256 | test("^[a-f0-9]{64}$")) and
    (.builderSHA256 | test("^[a-f0-9]{64}$")) and
    (.frontendSHA256 | test("^[a-f0-9]{64}$")) and
    (.toolchainSHA256 | test("^[a-f0-9]{64}$")) and
    (.roleRuntimeContractRevision | type == "number" and . > 0) and
    (.roleRuntimeContractSHA256 | test("^[a-f0-9]{64}$")) and
    (.platforms | type == "array" and length > 0 and length <= 8 and
      all(.[]; test("^linux/(amd64|arm64)(/[A-Za-z0-9][A-Za-z0-9._+~-]{0,127})?$")) and
      (unique | length) == length) and
    .policyRevision == $policy and .policySHA256 == $policy_sha and
    ($claim.stagingReference | endswith("@" + $claim.manifestDigest))
  ' /work/owner-claim.json >/dev/null || fail "owner admission claim is invalid"
  artifact_id=$(jq -er .artifactId /work/owner-claim.json)
  source_ref=$(jq -er .stagingReference /work/owner-claim.json)
  image_digest=$(jq -er .manifestDigest /work/owner-claim.json)
  image_hex=${image_digest#sha256:}
  spec_sha256=$(jq -er .specSHA256 /work/owner-claim.json)
  immutable_build_sha256=$(jq -er .immutableBuildSHA256 /work/owner-claim.json)
  expected_provenance_sha256=$(jq -er .provenanceSHA256 /work/owner-claim.json)
  base_image_digest=$(jq -er .baseImageDigest /work/owner-claim.json)
  source_sha256=$(jq -er .sourceSHA256 /work/owner-claim.json)
  context_sha256=$(jq -er .contextSHA256 /work/owner-claim.json)
  builder_sha256=$(jq -er .builderSHA256 /work/owner-claim.json)
  frontend_sha256=$(jq -er .frontendSHA256 /work/owner-claim.json)
  toolchain_sha256=$(jq -er .toolchainSHA256 /work/owner-claim.json)
  runtime_contract_revision=$(jq -er .roleRuntimeContractRevision /work/owner-claim.json)
  runtime_contract_sha256=$(jq -er .roleRuntimeContractSHA256 /work/owner-claim.json)
  [ "$runtime_contract_revision" = "$ROLE_RUNTIME_CONTRACT_REVISION" ] || fail "runtime contract revision mismatch"
  [ "$runtime_contract_sha256" = "$ROLE_RUNTIME_CONTRACT_SHA256" ] || fail "runtime contract digest mismatch"
  [ "$base_image_digest" = "$TRUSTED_ROLE_BASE_DIGEST" ] || fail "trusted role base digest mismatch"
  jq -r '.platforms[]' /work/owner-claim.json | sort -u >/work/expected-platforms
  staging_write_host=${source_ref%%/*}
  [ "$staging_write_host" = "mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001" ] ||
    fail "unexpected staging write host"
  source_ref="mattercodex-image-registry-staging-read.mattercodex-system.svc.cluster.local:5004/${source_ref#*/}"
  subject_name=${source_ref%@*}
  staging_host=${source_ref%%/*}
}

load_promotion_claim() {
  wait_for_file owner-promotion.json
  jq -e '
    . as $claim |
    (.artifactId | type == "string" and length > 0) and
    (.version | type == "number" and . > 0) and
    ((.claim | type == "string" and length > 0) or
      ((.claim // "") == "" and (.authorizationToken | type == "string" and length > 0))) and
    (.fence | type == "number" and . > 0) and
    (.expiresAt | type == "string" and length > 0) and
    (.stagingReference | test("^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*@sha256:[a-f0-9]{64}$")) and
    (.manifestDigest | test("^sha256:[a-f0-9]{64}$")) and
    (.admissionRevision | type == "number" and . > 0) and
    (.admissionReceiptSHA256 | test("^[a-f0-9]{64}$")) and
    (.admissionReceiptOCIManifestDigest | test("^sha256:[a-f0-9]{64}$")) and
    ($claim.stagingReference | endswith("@" + $claim.manifestDigest))
  ' /work/owner-promotion.json >/dev/null || fail "owner promotion claim is invalid"
  artifact_id=$(jq -er .artifactId /work/owner-promotion.json)
  source_ref=$(jq -er .stagingReference /work/owner-promotion.json)
  image_digest=$(jq -er .manifestDigest /work/owner-promotion.json)
  promotion_receipt=$(jq -er .admissionReceiptSHA256 /work/owner-promotion.json)
  staging_receipt_manifest_digest=$(jq -er .admissionReceiptOCIManifestDigest /work/owner-promotion.json)
  staging_write_host=${source_ref%%/*}
  [ "$staging_write_host" = "mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001" ] ||
    fail "unexpected staging write host"
  source_ref="mattercodex-image-registry-staging-read.mattercodex-system.svc.cluster.local:5004/${source_ref#*/}"
  subject_name=${source_ref%@*}
  staging_host=${source_ref%%/*}
}

claim_promotion() {
  remaining=120
  while [ "$remaining" -gt 0 ]; do
    if image-admission-bridge claim-promotion; then
      return 0
    fi
    remaining=$((remaining - 1))
    sleep 10
  done
  fail "owner promotion claim timeout"
}

publish_or_verify_receipt() {
  receipt_subject=$1
  receipt_tag=$2
  receipt_payload=$3
  receipt_sha256=$4
  receipt_manifest=$5
  receipt_type=application/vnd.mattercodex.image-admission-receipt.v1+json
  if regctl manifest get "$receipt_tag" --format raw-body >"$receipt_manifest" 2>/dev/null; then
    regctl artifact get "$receipt_tag" >"${receipt_payload}.readback"
    cmp -s "$receipt_payload" "${receipt_payload}.readback" ||
      fail "immutable admission receipt tag already points to another payload"
  else
    regctl artifact put --artifact-type "$receipt_type" --subject "$receipt_subject" \
      "$receipt_tag" <"$receipt_payload" >/dev/null
  fi
  regctl artifact get "$receipt_tag" >"${receipt_payload}.readback"
  cmp -s "$receipt_payload" "${receipt_payload}.readback" || fail "admission receipt OCI readback mismatch"
  regctl manifest get "$receipt_tag" --format raw-body >"$receipt_manifest"
  jq -e --arg type "$receipt_type" --arg digest "$image_digest" --arg receipt "$receipt_sha256" '
    .artifactType == $type and .subject.digest == $digest and
    (.layers | type == "array" and length == 1) and .layers[0].digest == ("sha256:" + $receipt)
  ' "$receipt_manifest" >/dev/null || fail "admission receipt OCI binding mismatch"
}

login_registry() {
  host=$1
  username_file=$2
  password_file=$3
  docker_directory=/tmp/docker
  mkdir -p "$docker_directory/certs.d/$host"
  cp /identity/ca.pem "$docker_directory/certs.d/$host/ca.crt"
  cp /identity/registry-client.crt "$docker_directory/certs.d/$host/client.cert"
  cp /identity/registry-client.key "$docker_directory/certs.d/$host/client.key"
  auth=$(printf '%s:%s' "$(tr -d '\r\n' <"$username_file")" \
    "$(tr -d '\r\n' <"$password_file")" | base64 | tr -d '\r\n')
  if [ -f "$docker_directory/config.json" ]; then
    jq --arg host "$host" --arg auth "$auth" '.auths[$host] = {auth:$auth}' \
      "$docker_directory/config.json" >"$docker_directory/config.next.json"
    mv "$docker_directory/config.next.json" "$docker_directory/config.json"
  else
    jq -n --arg host "$host" --arg auth "$auth" '{auths:{($host):{auth:$auth}}}' >"$docker_directory/config.json"
  fi
  export DOCKER_CONFIG=$docker_directory
  regctl registry set "$host" --tls enabled \
    --cacert "$(cat /identity/ca.pem)" \
    --client-cert "$(cat /identity/registry-client.crt)" \
    --client-key "$(cat /identity/registry-client.key)"
  regctl registry login "$host" --user "$(tr -d '\r\n' <"$username_file")" \
    --pass-stdin <"$password_file" >/dev/null
}

verify_image_and_provenance() {
  regctl manifest get "$source_ref" --format raw-body >/work/image-index.json
  jq -e '.manifests | type == "array" and length >= 2' /work/image-index.json >/dev/null ||
    fail "staging image is not an attested OCI index"
  jq -r '.manifests[] | select(.platform.os != "unknown" and .platform.architecture != "unknown") |
    .platform.os + "/" + .platform.architecture +
      (if (.platform.variant // "") == "" then "" else "/" + .platform.variant end)' \
    /work/image-index.json | sort -u >/work/actual-platforms
  cmp -s /work/expected-platforms /work/actual-platforms || fail "image platform set mismatch"
  jq -r '.manifests[] | select(.platform.os != "unknown" and .platform.architecture != "unknown") |
    [.digest, .platform.os, .platform.architecture, (.platform.variant // "")] | @tsv' \
    /work/image-index.json >/work/platform-manifests
  while IFS="$(printf '\t')" read -r platform_digest platform_os platform_arch platform_variant; do
    echo "$platform_digest" | grep -Eq '^sha256:[a-f0-9]{64}$' || fail "platform manifest digest is invalid"
    platform_ref="${subject_name}@${platform_digest}"
    regctl image inspect "$platform_ref" --format '{{json .Config.Labels}}' >/work/labels.json
    regctl image inspect "$platform_ref" --format '{{json .Config}}' >/work/image-config.json
    jq -e --arg spec "$spec_sha256" --arg immutable "$immutable_build_sha256" \
      --arg source "$source_sha256" --arg context "$context_sha256" --arg base "$base_image_digest" \
      --arg builder "$builder_sha256" --arg frontend "$frontend_sha256" --arg toolchain "$toolchain_sha256" \
      --arg policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" --arg runtime "$runtime_contract_sha256" '
      ."mattercodex.dev/spec-sha256" == $spec and
      ."mattercodex.dev/immutable-build-sha256" == $immutable and
      ."mattercodex.dev/source-sha256" == $source and
      ."mattercodex.dev/context-sha256" == $context and
      ."mattercodex.dev/base-image-digest" == $base and
      ."mattercodex.dev/builder-sha256" == $builder and
      ."mattercodex.dev/frontend-sha256" == $frontend and
      ."mattercodex.dev/toolchain-sha256" == $toolchain and
      ."mattercodex.dev/policy-revision" == $policy and
      ."mattercodex.dev/policy-sha256" == $policy_sha and
      ."mattercodex.dev/runtime-contract-sha256" == $runtime
    ' /work/labels.json >/dev/null || fail "build labels mismatch"
    verify_runtime_config /work/image-config.json
    [ "$(jq --arg image "$platform_digest" '[.manifests[] |
      select(.platform.os == "unknown" and .platform.architecture == "unknown") |
      select(.annotations["vnd.docker.reference.digest"] == $image)] | length' /work/image-index.json)" = 1 ] ||
      fail "native provenance manifest cardinality mismatch"
    attestation_digest=$(jq -er --arg image "$platform_digest" '.manifests[] |
      select(.platform.os == "unknown" and .platform.architecture == "unknown") |
      select(.annotations["vnd.docker.reference.digest"] == $image) | .digest' /work/image-index.json)
    regctl manifest get "${subject_name}@${attestation_digest}" --format raw-body >/work/provenance-manifest.json
    [ "$(jq '[.layers[] | select(.mediaType == "application/vnd.in-toto+json")] | length' \
      /work/provenance-manifest.json)" = 1 ] || fail "native provenance layer cardinality mismatch"
    provenance_layer=$(jq -er '.layers[] | select(.mediaType == "application/vnd.in-toto+json") | .digest' \
      /work/provenance-manifest.json)
    regctl blob get "$source_ref" "$provenance_layer" >/work/provenance.statement.json
    jq -e --arg image "${platform_digest#sha256:}" --arg base "${base_image_digest#sha256:}" \
      --arg frontend "$frontend_sha256" --arg builder_id "$EXPECTED_BUILDER_ID" \
      --arg build_type "$EXPECTED_BUILD_TYPE" -f /opt/mattercodex/provenance-policy.jq \
      /work/provenance.statement.json >/dev/null || fail "native provenance binding mismatch"
  done </work/platform-manifests
  jq -Sjc -n --arg build_type "$EXPECTED_BUILD_TYPE" --arg builder_id "$EXPECTED_BUILDER_ID" \
    --arg immutable "$immutable_build_sha256" --arg manifest "$image_digest" \
    --argjson policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" \
    --arg schema "mattercodex.dev/image-provenance-binding/v1" --arg spec "$spec_sha256" \
    '{buildType:$build_type,builderId:$builder_id,immutableBuildSHA256:$immutable,
      manifestDigest:$manifest,policyRevision:$policy,policySHA256:$policy_sha,
      schema:$schema,specSHA256:$spec}' >/work/provenance.binding.json
  cp /work/provenance.binding.json /work/provenance.json
  sha256sum /work/provenance.binding.json | awk '{print $1}' >/work/provenance.sha256
  [ "$(cat /work/provenance.sha256)" = "$expected_provenance_sha256" ] || fail "owner provenance digest mismatch"
}

if [ "${1:-}" = validate-runtime-config ]; then
  [ "$#" -eq 2 ] && [ -r "$2" ] || fail "runtime config fixture is invalid"
  verify_runtime_config "$2"
  exit 0
fi

require_policy

case "${1:-}" in
  claim)
    image-admission-bridge claim
    write_marker claim.complete
    ;;
  scan)
    wait_for_marker claim.complete
    load_owner_claim
    login_registry "$staging_host" /identity/username /identity/password
    [ "$(regctl image digest "$source_ref")" = "$image_digest" ] || fail "staging digest mismatch"
    verify_image_and_provenance
    syft "$source_ref" -o spdx-json=/work/sbom.json
    if grype sbom:/work/sbom.json --fail-on high -o json >/work/vulnerability.json; then
      printf '%s\n' ACCEPTED >/work/verdict
    else
      printf '%s\n' REJECTED >/work/verdict
    fi
    sha256sum /work/sbom.json | awk '{print $1}' >/work/sbom.sha256
    sha256sum /work/vulnerability.json | awk '{print $1}' >/work/vulnerability.sha256
    write_marker scan.complete
    ;;
  sign)
    wait_for_marker scan.complete
    load_owner_claim
    if [ "$(cat /work/verdict)" = ACCEPTED ]; then
      login_registry "$staging_host" /identity/username /identity/password
      verify_image_and_provenance
      export COSIGN_PASSWORD="$(cat /identity/cosign.password)"
      printf '%s\n' "$image_digest" >/work/image-digest.subject
      cosign sign-blob --yes --key /identity/cosign.key --output-signature /work/image-digest.sig /work/image-digest.subject
      for evidence in provenance sbom vulnerability; do
        cosign sign-blob --yes --key /identity/cosign.key --output-signature "/work/$evidence.sig" "/work/$evidence.json"
      done
    fi
    write_marker signature.complete
    ;;
  admit)
    wait_for_marker signature.complete
    load_owner_claim
    verdict=$(cat /work/verdict)
    signature_identity=not-applicable-rejected
    if [ "$verdict" = ACCEPTED ]; then
      login_registry "$staging_host" /identity/username /identity/password
      cosign verify-blob --key /identity/cosign.pub --signature /work/image-digest.sig /work/image-digest.subject \
        >/work/signature-verification.json
      for evidence in provenance sbom vulnerability; do
        cosign verify-blob --key /identity/cosign.pub --signature "/work/$evidence.sig" "/work/$evidence.json" \
          >"/work/$evidence-verification.json"
      done
      signature_identity=$(sha256sum /identity/cosign.pub | awk '{print $1}')
    fi
    jq -cn --arg image "$image_digest" --arg policy "$POLICY_REVISION" \
      --arg policy_sha "$POLICY_SHA256" --arg signature "$signature_identity" \
      --arg verdict "$verdict" \
      '{version:"v1",imageDigest:$image,policyRevision:$policy,policySHA256:$policy_sha,
        signatureIdentity:$signature,verdict:$verdict,verification:"cosign-key-v1"}' \
      >/work/signature.binding.json
    sha256sum /work/signature.binding.json | awk '{print $1}' >/work/signature.sha256
    jq -cn --arg artifact "$artifact_id" --arg image "$image_digest" --arg spec "$spec_sha256" \
      --arg immutable "$immutable_build_sha256" --arg provenance "$(cat /work/provenance.sha256)" \
      --arg sbom "$(cat /work/sbom.sha256)" --arg vulnerability "$(cat /work/vulnerability.sha256)" \
      --arg policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" --arg verdict "$verdict" \
      --arg signature "$signature_identity" --arg signature_sha "$(cat /work/signature.sha256)" \
      '{version:"v1",artifactId:$artifact,imageDigest:$image,specSHA256:$spec,
        immutableBuildSHA256:$immutable,provenanceSHA256:$provenance,sbomSHA256:$sbom,
        vulnerabilityEvidenceSHA256:$vulnerability,policyRevision:$policy,policySHA256:$policy_sha,
        verdict:$verdict,signatureIdentity:$signature,signatureSHA256:$signature_sha}' \
      >/work/admission.receipt.json
    sha256sum /work/admission.receipt.json | awk '{print $1}' >/work/admission.receipt.sha256
    jq -Sjc -n --arg subject "$image_digest" --arg receipt "$(cat /work/admission.receipt.sha256)" \
      '{schema:"mattercodex.dev/local-admission-receipt/v1",subjectDigest:$subject,receiptSHA256:$receipt}' \
      >/work/admission.receipt-manifest.json
    printf 'sha256:%s\n' "$(sha256sum /work/admission.receipt-manifest.json | awk '{print $1}')" \
      >/work/admission.receipt-manifest.digest
    IMAGE_OWNER_SBOM_SHA256_FILE=/work/sbom.sha256 \
    IMAGE_OWNER_VULNERABILITY_SHA256_FILE=/work/vulnerability.sha256 \
    IMAGE_OWNER_SIGNATURE_SHA256_FILE=/work/signature.sha256 \
    IMAGE_OWNER_ADMISSION_RECEIPT_SHA256_FILE=/work/admission.receipt.sha256 \
    IMAGE_OWNER_ADMISSION_RECEIPT_OCI_MANIFEST_DIGEST_FILE=/work/admission.receipt-manifest.digest \
    IMAGE_OWNER_SIGNATURE_IDENTITY="$signature_identity" IMAGE_OWNER_VERDICT="$verdict" \
      image-admission-bridge record
    write_marker admission.complete
    ;;
  promote)
    claim_promotion
    load_promotion_claim
    promotion_host=${PROMOTION_REPOSITORY%%/*}
    destination_tag="${PROMOTION_REPOSITORY}:artifact-${artifact_id}"
    promoted_receipt_tag="${PROMOTION_REPOSITORY}:admission-receipt-${artifact_id}"
    promotion_reference="${PROMOTION_REPOSITORY}@${image_digest}"
    promoted_reference="${PROMOTED_PULL_REPOSITORY}@${image_digest}"
    login_registry "$staging_host" /identity/staging.username /identity/staging.password
    [ "$(sha256sum /work/admission.receipt.json | awk '{print $1}')" = "$promotion_receipt" ] ||
      fail "staging admission receipt payload mismatch"
    [ "sha256:$(sha256sum /work/admission.receipt-manifest.json | awk '{print $1}')" = \
      "$staging_receipt_manifest_digest" ] || fail "staging admission receipt manifest mismatch"
    # Owner проверяет expiry/revocation/fence до первой pull-visible копии.
    image-admission-bridge authorize-promotion
    load_promotion_claim
    jq -e '(.authorizationToken | type == "string" and length > 0) and
      (.authorizationExpiresAt | type == "string" and length > 0) and (.claim == "")' \
      /work/owner-promotion.json >/dev/null || fail "owner promotion authorization is invalid"
    login_registry "$promotion_host" /identity/promotion.username /identity/promotion.password
    if current_digest=$(regctl image digest "$destination_tag" 2>/dev/null); then
      [ "$current_digest" = "$image_digest" ] || fail "immutable promotion tag already points to another digest"
    else
      regctl image copy "$source_ref" "$destination_tag"
    fi
    [ "$(regctl image digest "$promotion_reference")" = "$image_digest" ] || fail "promotion readback mismatch"
    regctl manifest get "$promotion_reference" --format raw-body >/work/promotion.image-manifest.json
    publish_or_verify_receipt "$promotion_reference" "$promoted_receipt_tag" /work/admission.receipt.json \
      "$promotion_receipt" /work/promoted.admission.receipt-manifest.json
    image_manifest_sha256=$(sha256sum /work/promotion.image-manifest.json | awk '{print $1}')
    receipt_manifest_sha256=$(sha256sum /work/promoted.admission.receipt-manifest.json | awk '{print $1}')
    jq -Sjc -n --arg image "$image_digest" --arg image_manifest "$image_manifest_sha256" \
      --arg receipt "$promotion_receipt" --arg staging_receipt_manifest "$staging_receipt_manifest_digest" \
      --arg promoted_receipt_manifest "$receipt_manifest_sha256" \
      '{imageManifestDigest:$image,imageManifestSHA256:$image_manifest,
        admissionReceiptSHA256:$receipt,stagingAdmissionReceiptManifestDigest:$staging_receipt_manifest,
        promotedAdmissionReceiptManifestSHA256:$promoted_receipt_manifest}' \
      >/work/promotion.readback.json
    sha256sum /work/promotion.readback.json | awk '{print $1}' >/work/promotion.readback.sha256
    IMAGE_OWNER_PROMOTED_REFERENCE="$promoted_reference" \
    IMAGE_OWNER_PROMOTION_READBACK_SHA256_FILE=/work/promotion.readback.sha256 \
      image-admission-bridge complete
    printf 'promoted image digest: %s\n' "$image_digest"
    ;;
  *) fail "unknown admission phase" ;;
esac
