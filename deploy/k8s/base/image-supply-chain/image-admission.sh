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
}

login_registry() {
  host=$1
  credential_dir=$2
  regctl registry set "$host" --tls enabled --cacert /registry-ca/ca.pem
  regctl registry login "$host" \
    --user "$(tr -d '\r\n' <"$credential_dir/username")" \
    --pass-stdin <"$credential_dir/password" >/dev/null
}

require_common
staging_host=mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001
promotion_host=mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003
source_ref="$staging_host/mattercodex/$IMAGE_NAME@$IMAGE_DIGEST"
destination_ref="$promotion_host/mattercodex/$IMAGE_NAME:$BUILD_TAG"

case "${1:-}" in
  scan)
    command -v regctl >/dev/null && command -v syft >/dev/null &&
      command -v grype >/dev/null && command -v cosign >/dev/null &&
      command -v jq >/dev/null || fail "admission tool image is incomplete"
    login_registry "$staging_host" /registry-push
    readback=$(regctl image digest "$source_ref")
    [ "$readback" = "$IMAGE_DIGEST" ] || fail "staging digest mismatch"
    regctl image inspect "$source_ref" --format '{{json .Config.Labels}}' > /work/labels.json
    jq -e --arg source "$SOURCE_DIGEST" \
      '."mattercodex.dev/source-sha256" == $source' /work/labels.json >/dev/null ||
      fail "source label mismatch"
    cosign download attestation "$source_ref" > /work/provenance.json
    grep -Fq "$SOURCE_DIGEST" /work/provenance.json || fail "provenance source mismatch"
    syft "$source_ref" -o spdx-json=/work/sbom.json
    if ! grype sbom:/work/sbom.json --fail-on high -o json > /work/vulnerability.json; then
      printf '%s\n' REJECTED > /work/verdict
      fail "vulnerability policy rejected image"
    fi
    printf '%s\n' ACCEPTED > /work/verdict
    sha256sum /work/provenance.json | awk '{print $1}' > /work/provenance.sha256
    sha256sum /work/sbom.json | awk '{print $1}' > /work/sbom.sha256
    sha256sum /work/vulnerability.json | awk '{print $1}' > /work/vulnerability.sha256
    touch /work/scan.complete
    ;;
  sign)
    [ -f /work/scan.complete ] && [ "$(cat /work/verdict)" = ACCEPTED ] ||
      fail "scanner evidence is absent"
    command -v cosign >/dev/null || fail "cosign is unavailable"
    login_registry "$staging_host" /registry-push
    export COSIGN_PASSWORD="$(cat /signing/cosign.password)"
    cosign sign --yes --key /signing/cosign.key "$source_ref"
    cosign attest --yes --key /signing/cosign.key \
      --type https://mattercodex.dev/attestation/provenance/v1 \
      --predicate /work/provenance.json "$source_ref"
    cosign attest --yes --key /signing/cosign.key \
      --type https://mattercodex.dev/attestation/sbom/v1 \
      --predicate /work/sbom.json "$source_ref"
    cosign attest --yes --key /signing/cosign.key \
      --type https://mattercodex.dev/attestation/vulnerability/v1 \
      --predicate /work/vulnerability.json "$source_ref"
    touch /work/signature.complete
    ;;
  admit)
    [ -f /work/signature.complete ] && [ "$(cat /work/verdict)" = ACCEPTED ] ||
      fail "signed evidence is absent"
    command -v cosign >/dev/null && command -v jq >/dev/null ||
      fail "admission verifier is unavailable"
    login_registry "$staging_host" /registry-push
    cosign verify --key /admission/cosign.pub "$source_ref" >/work/signature-verification.json
    for evidence in provenance sbom vulnerability; do
      cosign verify-attestation --key /admission/cosign.pub \
        --type "https://mattercodex.dev/attestation/$evidence/v1" \
        "$source_ref" > "/work/$evidence-verification.json"
    done
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
      --arg verdict ACCEPTED \
      --arg sbom_identity application/spdx+json \
      --arg scanner_identity "$ADMISSION_TOOLS_IMAGE" \
      --arg signature_identity "$(sha256sum /admission/cosign.pub | awk '{print $1}')" \
      --arg issued_at "$issued_at" \
      --arg expires_at "$expires_at" \
      '{version:$version,sourceDigest:$source,buildTag:$build,imageDigest:$image,
        provenanceDigest:$provenance,sbomDigest:$sbom,
        sbomIdentity:$sbom_identity,scannerIdentity:$scanner_identity,
        vulnerabilityEvidenceDigest:$vulnerability,
        vulnerabilityPolicyRevision:$policy,vulnerabilityVerdict:$verdict,
        signatureIdentitySHA256:$signature_identity,signatureVerified:true,
        issuedAt:$issued_at,expiresAt:$expires_at}' > /work/admission.receipt.json
    sha256sum /work/admission.receipt.json | awk '{print $1}' > /work/admission.receipt.sha256
    export COSIGN_PASSWORD="$(cat /admission/admission.password)"
    cosign sign-blob --yes --key /admission/admission.key \
      --output-signature /work/promotion.claim.sig /work/admission.receipt.json
    touch /work/admission.complete
    ;;
  promote)
    [ -f /work/admission.complete ] || fail "admission claim is absent"
    command -v cosign >/dev/null && command -v regctl >/dev/null &&
      command -v jq >/dev/null || fail "promotion verifier is unavailable"
    cosign verify-blob --key /admission-public/admission.pub \
      --signature /work/promotion.claim.sig /work/admission.receipt.json
    jq -e --arg source "$SOURCE_DIGEST" --arg build "$BUILD_TAG" \
      --arg image "$IMAGE_DIGEST" --arg policy "$POLICY_REVISION" \
      --arg scanner "$ADMISSION_TOOLS_IMAGE" \
      '.sourceDigest == $source and .buildTag == $build and
       .imageDigest == $image and .vulnerabilityPolicyRevision == $policy and
       .scannerIdentity == $scanner and .sbomIdentity == "application/spdx+json" and
       .vulnerabilityVerdict == "ACCEPTED" and .signatureVerified == true' \
      /work/admission.receipt.json >/dev/null || fail "promotion claim mismatch"
    expires_at=$(jq -r .expiresAt /work/admission.receipt.json)
    [ "$(date -u +%s)" -lt "$(date -u -d "$expires_at" +%s)" ] ||
      fail "promotion claim expired"
    login_registry "$staging_host" /registry-push
    login_registry "$promotion_host" /registry-promotion
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
