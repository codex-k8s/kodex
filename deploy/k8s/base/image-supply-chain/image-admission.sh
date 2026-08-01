#!/bin/sh
set -eu

fail() {
  echo "image admission failed: $1" >&2
  exit 1
}

require_common() {
  echo "$IMAGE_DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || fail "invalid image digest"
  echo "$SOURCE_DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || fail "invalid source digest"
  echo "$BUILD_TAG" | grep -Eq '^v[0-9]{14}-[a-f0-9]{40}$' || fail "invalid build tag"
  echo "$POLICY_REVISION" | grep -Eq '^[1-9][0-9]*$' || fail "invalid policy revision"
  echo "$IMAGE_NAME" | grep -Eq '^[a-z0-9]+([._-][a-z0-9]+)*$' || fail "invalid image name"
  echo "$ADMISSION_TOOLS_IMAGE" |
    grep -Eq '^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$' ||
    fail "admission tools image is not immutable"
  echo "$ADMISSION_TOOLS_SHA256" | grep -Eq '^sha256:[a-f0-9]{64}$' ||
    fail "invalid admission tools digest"
  [ "${ADMISSION_TOOLS_IMAGE##*@}" = "$ADMISSION_TOOLS_SHA256" ] ||
    fail "admission tools digest mismatch"
  echo "$ADMISSION_ATTEMPT_SHA256" | grep -Eq '^[a-f0-9]{64}$' ||
    fail "invalid admission attempt digest"
  [ "$EXPECTED_BUILDER_ID" = "spiffe://mattercodex.local/ns/mattercodex-system/sa/mattercodex-role-image-builder" ] ||
    fail "untrusted builder identity"
  [ "$EXPECTED_BUILD_TYPE" = "https://mobyproject.org/buildkit@v1" ] ||
    fail "untrusted build type"
  for identity in "$SCANNER_IDENTITY" "$SIGNER_IDENTITY" "$ADMISSION_OWNER_IDENTITY" "$PROMOTION_IDENTITY"; do
    echo "$identity" | grep -Eq '^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$' ||
      fail "invalid admission phase identity"
  done
  computed_attempt=$(printf '%s\n' "$SOURCE_DIGEST" "$BUILD_TAG" "$IMAGE_DIGEST" \
    "$ADMISSION_TOOLS_SHA256" "$POLICY_REVISION" "$EXPECTED_BUILDER_ID" \
    "$EXPECTED_BUILD_TYPE" "$SCANNER_IDENTITY" "$SIGNER_IDENTITY" \
    "$ADMISSION_OWNER_IDENTITY" "$PROMOTION_IDENTITY" |
    sha256sum | awk '{print $1}')
  [ "$computed_attempt" = "$ADMISSION_ATTEMPT_SHA256" ] ||
    fail "admission attempt tuple mismatch"
  for tool in base64 buildctl cmp cosign curl date grype jq openssl pgrep regctl sha256sum syft; do
    command -v "$tool" >/dev/null || fail "admission tool image is incomplete"
  done
}

wait_for_evidence() {
  marker=$1
  remaining=150
  while [ ! -f "/work/$marker" ] && [ "$remaining" -gt 0 ]; do
    sleep 10
    remaining=$((remaining - 1))
  done
  [ -f "/work/$marker" ] || fail "predecessor evidence timeout"
  [ "$(cat "/work/$marker")" = "$ADMISSION_ATTEMPT_SHA256" ] ||
    fail "stale predecessor evidence"
}

write_marker() {
  printf '%s\n' "$ADMISSION_ATTEMPT_SHA256" >"/work/$1"
}

validate_evidence_manifest() {
  builder_key_sha256=$(sha256sum /identity/builder.pub | awk '{print $1}')
  jq -e --arg source "$SOURCE_DIGEST" --arg build "$BUILD_TAG" \
    --arg image "$IMAGE_DIGEST" --arg tools "$ADMISSION_TOOLS_SHA256" \
    --arg policy "$POLICY_REVISION" --arg attempt "$ADMISSION_ATTEMPT_SHA256" \
    --arg builder "$EXPECTED_BUILDER_ID" --arg build_type "$EXPECTED_BUILD_TYPE" \
    --arg builder_key "$builder_key_sha256" \
    --arg scanner "$SCANNER_IDENTITY" --arg signer "$SIGNER_IDENTITY" \
    --arg owner "$ADMISSION_OWNER_IDENTITY" --arg promotion "$PROMOTION_IDENTITY" '
      .sourceDigest == $source and .buildTag == $build and
      .imageDigest == $image and .toolsDigest == $tools and
      .policyRevision == $policy and .attemptSHA256 == $attempt and
      .builderIdentity == $builder and .buildType == $build_type and
      .builderSignatureIdentitySHA256 == $builder_key and
      .scannerIdentity == $scanner and .signerIdentity == $signer and
      .admissionOwnerIdentity == $owner and .promotionIdentity == $promotion and
      (.resolvedDependenciesSHA256 | type == "string" and test("^[a-f0-9]{64}$"))
    ' /work/evidence.manifest.json >/dev/null || fail "evidence tuple mismatch"
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
    jq --arg host "$host" --arg auth "$auth" \
      '.auths[$host] = {auth:$auth}' "$docker_directory/config.json" \
      > "$docker_directory/config.next.json"
    mv "$docker_directory/config.next.json" "$docker_directory/config.json"
  else
    jq -n --arg host "$host" --arg auth "$auth" \
      '{auths:{($host):{auth:$auth}}}' > "$docker_directory/config.json"
  fi
  export DOCKER_CONFIG=$docker_directory
  regctl registry set "$host" --tls enabled \
    --cacert "$(cat /identity/ca.pem)" \
    --client-cert "$(cat /identity/registry-client.crt)" \
    --client-key "$(cat /identity/registry-client.key)"
  regctl registry login "$host" \
    --user "$(tr -d '\r\n' <"$username_file")" \
    --pass-stdin <"$password_file" >/dev/null
}

validate_provenance_statement() {
  statement=$1
  image_hex=${IMAGE_DIGEST#sha256:}
  jq -e --arg image "$image_hex" --arg source "$SOURCE_DIGEST" \
    --arg subject "$subject_name" --arg build_tag "$BUILD_TAG" \
    --arg tools_digest "$ADMISSION_TOOLS_SHA256" \
    --arg policy_revision "$POLICY_REVISION" \
    --arg builder_id "$EXPECTED_BUILDER_ID" \
    --arg build_type "$EXPECTED_BUILD_TYPE" \
    -f /opt/mattercodex/provenance-policy.jq "$statement" >/dev/null ||
    fail "provenance semantic binding mismatch"
}

decode_signed_provenance() {
  envelope=$1
  statement=$2
  expected_type=$3
  decoded="${statement}.decoded"
  image_hex=${IMAGE_DIGEST#sha256:}
  jq -er 'select(.payloadType == "application/vnd.in-toto+json") | .payload' "$envelope" |
    base64 -d >"$decoded" || fail "signed provenance DSSE decode failed"
  jq -e --arg image "$image_hex" --arg expected_type "$expected_type" '
    ._type == "https://in-toto.io/Statement/v1" and
    .predicateType == $expected_type and
    ([.subject[]? | select(.digest.sha256 == $image)] | length == 1) and
    (.predicate | type == "object")
  ' "$decoded" >/dev/null || fail "signed provenance envelope mismatch"
  jq -S -c .predicate "$decoded" >"$statement" ||
    fail "signed provenance predicate decode failed"
  rm -f "$decoded"
  validate_provenance_statement "$statement"
}

verify_trusted_build() {
  cosign verify-attestation --key /identity/builder.pub \
    --type https://mattercodex.dev/attestation/trusted-build/v1 \
    "$source_ref" > /work/trusted-build-verification.json
  decode_signed_provenance /work/trusted-build-verification.json \
    /work/provenance.trusted.json \
    https://mattercodex.dev/attestation/trusted-build/v1
}

require_common
staging_host=mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001
promotion_host=mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003
source_ref="$staging_host/mattercodex/$IMAGE_NAME@$IMAGE_DIGEST"
subject_name="$staging_host/mattercodex/$IMAGE_NAME"
destination_ref="$promotion_host/mattercodex/$IMAGE_NAME:$BUILD_TAG"

case "${1:-}" in
  scan)
    login_registry "$staging_host" /identity/username /identity/password
    readback=$(regctl image digest "$source_ref")
    [ "$readback" = "$IMAGE_DIGEST" ] || fail "staging digest mismatch"
    regctl image inspect "$source_ref" --format '{{json .Config.Labels}}' > /work/labels.json
    jq -e --arg source "$SOURCE_DIGEST" --arg build "$BUILD_TAG" \
      --arg tools "$ADMISSION_TOOLS_SHA256" --arg policy "$POLICY_REVISION" '
        ."mattercodex.dev/source-sha256" == $source and
        ."mattercodex.dev/build-tag" == $build and
        ."mattercodex.dev/admission-tools-sha256" == $tools and
        ."mattercodex.dev/admission-policy-revision" == $policy
      ' /work/labels.json >/dev/null || fail "build labels mismatch"
    verify_trusted_build
    cp /work/provenance.trusted.json /work/provenance.json
    syft "$source_ref" -o spdx-json=/work/sbom.json
    if ! grype sbom:/work/sbom.json --fail-on high -o json > /work/vulnerability.json; then
      printf '%s\n' REJECTED > /work/verdict
      fail "vulnerability policy rejected image"
    fi
    printf '%s\n' ACCEPTED > /work/verdict
    sha256sum /work/provenance.json | awk '{print $1}' > /work/provenance.sha256
    sha256sum /work/sbom.json | awk '{print $1}' > /work/sbom.sha256
    sha256sum /work/vulnerability.json | awk '{print $1}' > /work/vulnerability.sha256
    jq -S -c '.predicate.buildDefinition.resolvedDependencies' /work/provenance.json |
      sha256sum | awk '{print $1}' > /work/resolved-dependencies.sha256
    jq -cn --arg source "$SOURCE_DIGEST" --arg build "$BUILD_TAG" \
      --arg image "$IMAGE_DIGEST" --arg tools "$ADMISSION_TOOLS_SHA256" \
      --arg policy "$POLICY_REVISION" --arg attempt "$ADMISSION_ATTEMPT_SHA256" \
      --arg builder "$EXPECTED_BUILDER_ID" --arg build_type "$EXPECTED_BUILD_TYPE" \
      --arg builder_key "$(sha256sum /identity/builder.pub | awk '{print $1}')" \
      --arg scanner "$SCANNER_IDENTITY" --arg signer "$SIGNER_IDENTITY" \
      --arg owner "$ADMISSION_OWNER_IDENTITY" --arg promotion "$PROMOTION_IDENTITY" \
      --arg dependencies "$(cat /work/resolved-dependencies.sha256)" \
      '{sourceDigest:$source,buildTag:$build,imageDigest:$image,toolsDigest:$tools,
        policyRevision:$policy,attemptSHA256:$attempt,builderIdentity:$builder,
        buildType:$build_type,builderSignatureIdentitySHA256:$builder_key,
        scannerIdentity:$scanner,signerIdentity:$signer,
        admissionOwnerIdentity:$owner,promotionIdentity:$promotion,
        resolvedDependenciesSHA256:$dependencies}' > /work/evidence.manifest.json
    validate_evidence_manifest
    write_marker scan.complete
    ;;
  sign)
    wait_for_evidence scan.complete
    validate_evidence_manifest
    [ "$(cat /work/verdict)" = ACCEPTED ] ||
      fail "scanner evidence is absent"
    login_registry "$staging_host" /identity/username /identity/password
    verify_trusted_build
    [ "$(sha256sum /work/provenance.trusted.json | awk '{print $1}')" = \
      "$(cat /work/provenance.sha256)" ] || fail "builder provenance changed"
    export COSIGN_PASSWORD="$(cat /identity/cosign.password)"
    cosign sign --yes --key /identity/cosign.key "$source_ref"
    cosign attest --yes --key /identity/cosign.key \
      --type https://mattercodex.dev/attestation/provenance/v1 \
      --predicate /work/provenance.json "$source_ref"
    cosign attest --yes --key /identity/cosign.key \
      --type https://mattercodex.dev/attestation/sbom/v1 \
      --predicate /work/sbom.json "$source_ref"
    cosign attest --yes --key /identity/cosign.key \
      --type https://mattercodex.dev/attestation/vulnerability/v1 \
      --predicate /work/vulnerability.json "$source_ref"
    write_marker signature.complete
    ;;
  admit)
    wait_for_evidence signature.complete
    validate_evidence_manifest
    [ "$(cat /work/verdict)" = ACCEPTED ] ||
      fail "signed evidence is absent"
    login_registry "$staging_host" /identity/username /identity/password
    verify_trusted_build
    [ "$(sha256sum /work/provenance.trusted.json | awk '{print $1}')" = \
      "$(cat /work/provenance.sha256)" ] || fail "builder provenance changed"
    cosign verify --key /identity/cosign.pub "$source_ref" >/work/signature-verification.json
    for evidence in provenance sbom vulnerability; do
      cosign verify-attestation --key /identity/cosign.pub \
        --type "https://mattercodex.dev/attestation/$evidence/v1" \
        "$source_ref" > "/work/$evidence-verification.json"
    done
    decode_signed_provenance \
      /work/provenance-verification.json /work/provenance-verified.json \
      https://mattercodex.dev/attestation/provenance/v1
    [ "$(sha256sum /work/provenance-verified.json | awk '{print $1}')" = \
      "$(cat /work/provenance.sha256)" ] || fail "verified provenance digest mismatch"
    issued_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    expires_at=$(date -u -d '+10 minutes' +%Y-%m-%dT%H:%M:%SZ)
    jq -cn \
      --arg version v1 \
      --arg source "$SOURCE_DIGEST" \
      --arg build "$BUILD_TAG" \
      --arg image "$IMAGE_DIGEST" \
      --arg provenance "$(cat /work/provenance.sha256)" \
      --arg sbom "$(cat /work/sbom.sha256)" \
      --arg vulnerability "$(cat /work/vulnerability.sha256)" \
      --arg policy "$POLICY_REVISION" \
      --arg tools "$ADMISSION_TOOLS_SHA256" \
      --arg attempt "$ADMISSION_ATTEMPT_SHA256" \
      --arg builder "$EXPECTED_BUILDER_ID" \
      --arg build_type "$EXPECTED_BUILD_TYPE" \
      --arg builder_key "$(sha256sum /identity/builder.pub | awk '{print $1}')" \
      --arg dependencies "$(cat /work/resolved-dependencies.sha256)" \
      --arg scanner_owner "$SCANNER_IDENTITY" \
      --arg signer_owner "$SIGNER_IDENTITY" \
      --arg admission_owner "$ADMISSION_OWNER_IDENTITY" \
      --arg promotion_owner "$PROMOTION_IDENTITY" \
      --arg verdict ACCEPTED \
      --arg sbom_identity application/spdx+json \
      --arg scanner_identity "$ADMISSION_TOOLS_IMAGE" \
      --arg signature_identity "$(sha256sum /identity/cosign.pub | awk '{print $1}')" \
      --arg issued_at "$issued_at" \
      --arg expires_at "$expires_at" \
      '{version:$version,sourceDigest:$source,buildTag:$build,imageDigest:$image,
        provenanceDigest:$provenance,sbomDigest:$sbom,
        sbomIdentity:$sbom_identity,scannerIdentity:$scanner_identity,
        vulnerabilityEvidenceDigest:$vulnerability,
        vulnerabilityPolicyRevision:$policy,vulnerabilityVerdict:$verdict,
        admissionToolsSHA256:$tools,admissionAttemptSHA256:$attempt,
        builderIdentity:$builder,buildType:$build_type,
        builderSignatureIdentitySHA256:$builder_key,
        resolvedDependenciesSHA256:$dependencies,
        scannerOwnerIdentity:$scanner_owner,signerOwnerIdentity:$signer_owner,
        admissionOwnerIdentity:$admission_owner,promotionIdentity:$promotion_owner,
        signatureIdentitySHA256:$signature_identity,signatureVerified:true,
        issuedAt:$issued_at,expiresAt:$expires_at}' > /work/admission.receipt.json
    sha256sum /work/admission.receipt.json | awk '{print $1}' > /work/admission.receipt.sha256
    export COSIGN_PASSWORD="$(cat /identity/admission.password)"
    cosign sign-blob --yes --key /identity/admission.key \
      --output-signature /work/promotion.claim.sig /work/admission.receipt.json
    write_marker admission.complete
    ;;
  promote)
    wait_for_evidence admission.complete
    validate_evidence_manifest
    cosign verify-blob --key /identity/admission.pub \
      --signature /work/promotion.claim.sig /work/admission.receipt.json
    jq -e --arg source "$SOURCE_DIGEST" --arg build "$BUILD_TAG" \
      --arg image "$IMAGE_DIGEST" --arg policy "$POLICY_REVISION" \
      --arg scanner "$ADMISSION_TOOLS_IMAGE" --arg tools "$ADMISSION_TOOLS_SHA256" \
      --arg attempt "$ADMISSION_ATTEMPT_SHA256" --arg builder "$EXPECTED_BUILDER_ID" \
      --arg build_type "$EXPECTED_BUILD_TYPE" --arg scanner_owner "$SCANNER_IDENTITY" \
      --arg builder_key "$(jq -r .builderSignatureIdentitySHA256 /work/evidence.manifest.json)" \
      --arg dependencies "$(jq -r .resolvedDependenciesSHA256 /work/evidence.manifest.json)" \
      --arg signer_owner "$SIGNER_IDENTITY" --arg admission_owner "$ADMISSION_OWNER_IDENTITY" \
      --arg promotion_owner "$PROMOTION_IDENTITY" \
      '.sourceDigest == $source and .buildTag == $build and
       .imageDigest == $image and .vulnerabilityPolicyRevision == $policy and
       .scannerIdentity == $scanner and .sbomIdentity == "application/spdx+json" and
       .admissionToolsSHA256 == $tools and .admissionAttemptSHA256 == $attempt and
       .builderIdentity == $builder and .buildType == $build_type and
       .builderSignatureIdentitySHA256 == $builder_key and
       .resolvedDependenciesSHA256 == $dependencies and
       .scannerOwnerIdentity == $scanner_owner and .signerOwnerIdentity == $signer_owner and
       .admissionOwnerIdentity == $admission_owner and .promotionIdentity == $promotion_owner and
       .vulnerabilityVerdict == "ACCEPTED" and .signatureVerified == true' \
      /work/admission.receipt.json >/dev/null || fail "promotion claim mismatch"
    expires_at=$(jq -r .expiresAt /work/admission.receipt.json)
    [ "$(date -u +%s)" -lt "$(date -u -d "$expires_at" +%s)" ] ||
      fail "promotion claim expired"
    login_registry "$staging_host" /identity/staging.username /identity/staging.password
    login_registry "$promotion_host" /identity/promotion.username /identity/promotion.password
    if current_digest=$(regctl image digest "$destination_ref" 2>/dev/null); then
      [ "$current_digest" = "$IMAGE_DIGEST" ] ||
        fail "immutable promotion tag already points to another digest"
    else
      regctl image copy "$source_ref" "$destination_ref"
    fi
    [ "$(regctl image digest "$destination_ref")" = "$IMAGE_DIGEST" ] ||
      fail "promotion readback mismatch"
    receipt_sha256=$(cat /work/admission.receipt.sha256)
    issued_at=$(jq -r .issuedAt /work/admission.receipt.json)
    regctl artifact put --subject "$destination_ref" \
      --artifact-type application/vnd.mattercodex.image-admission.v1+json \
      --annotation "org.opencontainers.image.created=$issued_at" \
      --annotation "mattercodex.dev/admission-receipt-sha256=$receipt_sha256" \
      --by-digest --file /work/admission.receipt.json \
      >/work/admission-artifact.digest
    grep -Eq '^sha256:[a-f0-9]{64}$' /work/admission-artifact.digest ||
      fail "admission receipt readback is invalid"
    regctl artifact get --subject "$destination_ref" \
      --filter-artifact-type application/vnd.mattercodex.image-admission.v1+json \
      --latest >/work/admission.readback.json
    [ "$(sha256sum /work/admission.readback.json | awk '{print $1}')" = "$receipt_sha256" ] ||
      fail "admission receipt content mismatch"
    printf 'admitted image digest: %s\n' "$IMAGE_DIGEST"
    ;;
  *) fail "unknown admission phase" ;;
esac
