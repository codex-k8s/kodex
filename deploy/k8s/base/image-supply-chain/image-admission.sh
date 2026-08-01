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
  for tool in base64 cmp cosign curl date grype jq openssl pgrep regctl sha256sum syft; do
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
}

login_registry() {
  host=$1
  username_file=$2
  password_file=$3
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
    -f /opt/mattercodex/provenance-policy.jq "$statement" >/dev/null ||
    fail "provenance semantic binding mismatch"
}

decode_and_validate_provenance() {
  envelope=$1
  statement=$2
  decoded="${statement}.decoded"
  jq -er 'select(.payloadType == "application/vnd.in-toto+json") | .payload' "$envelope" |
    base64 -d >"$decoded" || fail "provenance DSSE decode failed"
  validate_provenance_statement "$decoded"
  jq -S -c . "$decoded" >"$statement" || fail "provenance normalization failed"
  rm -f "$decoded"
}

decode_signed_provenance() {
  envelope=$1
  statement=$2
  decoded="${statement}.decoded"
  image_hex=${IMAGE_DIGEST#sha256:}
  jq -er 'select(.payloadType == "application/vnd.in-toto+json") | .payload' "$envelope" |
    base64 -d >"$decoded" || fail "signed provenance DSSE decode failed"
  jq -e --arg image "$image_hex" '
    ._type == "https://in-toto.io/Statement/v1" and
    .predicateType == "https://mattercodex.dev/attestation/provenance/v1" and
    ([.subject[]? | select(.digest.sha256 == $image)] | length == 1) and
    (.predicate | type == "object")
  ' "$decoded" >/dev/null || fail "signed provenance envelope mismatch"
  jq -S -c .predicate "$decoded" >"$statement" ||
    fail "signed provenance predicate decode failed"
  rm -f "$decoded"
  validate_provenance_statement "$statement"
}

require_common
staging_host=mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001
promotion_host=mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003
source_ref="$staging_host/mattercodex/$IMAGE_NAME@$IMAGE_DIGEST"
destination_ref="$promotion_host/mattercodex/$IMAGE_NAME:$BUILD_TAG"

case "${1:-}" in
  scan)
    login_registry "$staging_host" /identity/username /identity/password
    readback=$(regctl image digest "$source_ref")
    [ "$readback" = "$IMAGE_DIGEST" ] || fail "staging digest mismatch"
    regctl image inspect "$source_ref" --format '{{json .Config.Labels}}' > /work/labels.json
    jq -e --arg source "$SOURCE_DIGEST" \
      '."mattercodex.dev/source-sha256" == $source' /work/labels.json >/dev/null ||
      fail "source label mismatch"
    cosign download attestation "$source_ref" > /work/provenance.dsse.json
    decode_and_validate_provenance /work/provenance.dsse.json /work/provenance.json
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
    wait_for_evidence scan.complete
    [ "$(cat /work/verdict)" = ACCEPTED ] ||
      fail "scanner evidence is absent"
    login_registry "$staging_host" /identity/username /identity/password
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
    touch /work/signature.complete
    ;;
  admit)
    wait_for_evidence signature.complete
    [ "$(cat /work/verdict)" = ACCEPTED ] ||
      fail "signed evidence is absent"
    login_registry "$staging_host" /identity/username /identity/password
    cosign verify --key /identity/cosign.pub "$source_ref" >/work/signature-verification.json
    for evidence in provenance sbom vulnerability; do
      cosign verify-attestation --key /identity/cosign.pub \
        --type "https://mattercodex.dev/attestation/$evidence/v1" \
        "$source_ref" > "/work/$evidence-verification.json"
    done
    decode_signed_provenance \
      /work/provenance-verification.json /work/provenance-verified.json
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
        signatureIdentitySHA256:$signature_identity,signatureVerified:true,
        issuedAt:$issued_at,expiresAt:$expires_at}' > /work/admission.receipt.json
    sha256sum /work/admission.receipt.json | awk '{print $1}' > /work/admission.receipt.sha256
    export COSIGN_PASSWORD="$(cat /identity/admission.password)"
    cosign sign-blob --yes --key /identity/admission.key \
      --output-signature /work/promotion.claim.sig /work/admission.receipt.json
    touch /work/admission.complete
    ;;
  promote)
    wait_for_evidence admission.complete
    cosign verify-blob --key /identity/admission.pub \
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
