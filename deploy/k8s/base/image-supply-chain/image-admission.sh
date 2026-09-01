#!/bin/sh
set -eu

admission_phase=${1:-}

fail() {
  if [ "$admission_phase" = scan ] || [ "$admission_phase" = sign ]; then
    write_technical_rejection "$1" || true
  fi
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
  [ "$EVIDENCE_REPOSITORY" = "kodex-image-registry-evidence.kodex-system.svc.cluster.local:5007/evidence/role-image-admission" ] ||
    fail "evidence repository is invalid"
  [ "$PROMOTION_EVIDENCE_REPOSITORY" = "kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/evidence" ] ||
    fail "promotion evidence repository is invalid"
  echo "$PROMOTED_PULL_REPOSITORY" | grep -Eq '^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$' ||
    fail "promoted pull repository is invalid"
  [ "${PROMOTION_REPOSITORY#*/}" = "${PROMOTED_PULL_REPOSITORY#*/}" ] ||
    fail "promotion and pull repository paths differ"
  [ "$EXPECTED_BUILDER_ID" = "spiffe://kodex.local/ns/kodex-system/sa/role-image-builder" ] ||
    fail "untrusted builder identity"
  [ "$EXPECTED_BUILD_TYPE" = "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md" ] ||
    fail "untrusted build type"
  echo "$ROLE_RUNTIME_CONTRACT_REVISION" | grep -Eq '^[1-9][0-9]*$' || fail "invalid runtime contract revision"
  echo "$ROLE_RUNTIME_CONTRACT_SHA256" | grep -Eq '^[a-f0-9]{64}$' || fail "invalid runtime contract digest"
  echo "$TRUSTED_ROLE_BASE_REPOSITORY" | grep -Eq '^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$' ||
    fail "trusted role base repository is invalid"
  echo "$TRUSTED_ROLE_BASE_DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || fail "trusted role base digest is invalid"
  for tool in base64 cmp cosign grype image-admission-bridge jq regctl sha256sum syft wc; do
    command -v "$tool" >/dev/null || fail "admission image is incomplete"
  done
}

verify_runtime_config() {
  config_file=$1
  jq -e '
    .User == "10001:10001" and
    .Entrypoint == ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"] and
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
  [ "$staging_write_host" = "kodex-image-registry-push.kodex-system.svc.cluster.local:5001" ] ||
    fail "unexpected staging write host"
  source_ref="kodex-image-registry-staging-read.kodex-system.svc.cluster.local:5004/${source_ref#*/}"
  subject_name=${source_ref%@*}
  staging_host=${source_ref%%/*}
}

write_technical_rejection() {
  reason=$1
  [ -f /work/owner-claim.json ] || return 0
  jq -e --argjson policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" '
    (.artifactId | type == "string" and length > 0) and
    (.manifestDigest | test("^sha256:[a-f0-9]{64}$")) and
    (.specSHA256 | test("^[a-f0-9]{64}$")) and
    (.immutableBuildSHA256 | test("^[a-f0-9]{64}$")) and
    .policyRevision == $policy and .policySHA256 == $policy_sha
  ' /work/owner-claim.json >/dev/null || return 0
  image_digest=$(jq -er .manifestDigest /work/owner-claim.json)
  spec_sha256=$(jq -er .specSHA256 /work/owner-claim.json)
  immutable_build_sha256=$(jq -er .immutableBuildSHA256 /work/owner-claim.json)
  jq -Sjc -n --arg build_type "$EXPECTED_BUILD_TYPE" --arg builder_id "$EXPECTED_BUILDER_ID" \
    --arg immutable "$immutable_build_sha256" --arg manifest "$image_digest" \
    --argjson policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" \
    --arg schema "kodex.dev/image-provenance-binding/v1" --arg spec "$spec_sha256" \
    '{buildType:$build_type,builderId:$builder_id,immutableBuildSHA256:$immutable,
      manifestDigest:$manifest,policyRevision:$policy,policySHA256:$policy_sha,
      schema:$schema,specSHA256:$spec}' >/work/provenance.json
  jq -Sjc -n --arg phase "$admission_phase" --arg reason "$reason" \
    '[{schema:"kodex.dev/native-provenance-rejection/v1",phase:$phase,reason:$reason}]' \
    >/work/native-provenance.json
  jq -Sjc -n --arg phase "$admission_phase" --arg reason "$reason" \
    '{schema:"kodex.dev/sbom-unavailable/v1",phase:$phase,reason:$reason}' >/work/sbom.json
  jq -Sjc -n --arg phase "$admission_phase" --arg reason "$reason" \
    '{schema:"kodex.dev/vulnerability-evidence-unavailable/v1",phase:$phase,reason:$reason}' \
    >/work/vulnerability.json
  sha256sum /work/provenance.json | awk '{print $1}' >/work/provenance.sha256
  sha256sum /work/sbom.json | awk '{print $1}' >/work/sbom.sha256
  sha256sum /work/vulnerability.json | awk '{print $1}' >/work/vulnerability.sha256
  printf '%s\n' REJECTED >/work/verdict
  write_marker signature.complete
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
  [ "$staging_write_host" = "kodex-image-registry-push.kodex-system.svc.cluster.local:5001" ] ||
    fail "unexpected staging write host"
  source_ref="kodex-image-registry-staging-read.kodex-system.svc.cluster.local:5004/${source_ref#*/}"
  subject_name=${source_ref%@*}
  staging_host=${source_ref%%/*}
}

claim_promotion() {
  remaining=120
  while [ "$remaining" -gt 0 ]; do
    if image-admission-bridge claim-promotion 2>/dev/null; then
      return 0
    fi
    remaining=$((remaining - 1))
    sleep 5
  done
  fail "owner promotion work is unavailable"
}

claim_admission() {
  remaining=120
  while [ "$remaining" -gt 0 ]; do
    if image-admission-bridge claim 2>/dev/null; then
      return 0
    fi
    remaining=$((remaining - 1))
    sleep 5
  done
  fail "owner admission work is unavailable"
}

evidence_entries() {
  cat <<'EOF'
image-digest.subject|application/vnd.kodex.image-digest.v1+text
image-digest.sig|application/vnd.dev.cosign.signature.v1+text
provenance.json|application/vnd.kodex.provenance-binding.v1+json
provenance.sig|application/vnd.dev.cosign.signature.v1+text
native-provenance.json|application/vnd.kodex.native-provenance.v1+json
native-provenance.sig|application/vnd.dev.cosign.signature.v1+text
sbom.json|application/spdx+json
sbom.sig|application/vnd.dev.cosign.signature.v1+text
vulnerability.json|application/vnd.kodex.vulnerability-report.v1+json
vulnerability.sig|application/vnd.dev.cosign.signature.v1+text
signature.binding.json|application/vnd.kodex.signature-binding.v1+json
admission.receipt.json|application/vnd.kodex.admission-receipt.v1+json
cosign.pub|application/vnd.dev.cosign.public-key.v1+pem
EOF
}

verify_evidence_manifest() {
  evidence_manifest=$1
  expected_manifest_digest=$2
  expected_artifact=$3
  expected_image=$4
  expected_policy_revision=$5
  expected_policy_sha256=$6
  expected_entries=$(evidence_entries | jq -Rn \
    '[inputs | split("|") | {key:.[0],value:.[1]}] | from_entries')
  [ "sha256:$(sha256sum "$evidence_manifest" | awk '{print $1}')" = "$expected_manifest_digest" ] ||
    fail "admission evidence OCI manifest digest mismatch"
  jq -e --arg artifact "$expected_artifact" --arg image "$expected_image" \
    --arg policy "$expected_policy_revision" --arg policy_sha "$expected_policy_sha256" \
    --argjson expected "$expected_entries" '
    (. | keys | sort) == (["annotations","artifactType","config","layers","mediaType","schemaVersion"] | sort) and
    .schemaVersion == 2 and .mediaType == "application/vnd.oci.image.manifest.v1+json" and
    .artifactType == "application/vnd.kodex.image-admission-evidence.v2" and
    .config == {mediaType:"application/vnd.kodex.image-admission-evidence.config.v2+json",
      digest:"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",size:2} and
    (.annotations | keys | sort) == (["kodex.dev/artifact-id","kodex.dev/evidence-schema",
      "kodex.dev/image-digest","kodex.dev/policy-revision","kodex.dev/policy-sha256"] | sort) and
    .annotations["kodex.dev/evidence-schema"] == "kodex.dev/image-admission-evidence/v2" and
    .annotations["kodex.dev/artifact-id"] == $artifact and
    .annotations["kodex.dev/image-digest"] == $image and
    .annotations["kodex.dev/policy-revision"] == $policy and
    .annotations["kodex.dev/policy-sha256"] == $policy_sha and
    (.layers | type == "array" and length == ($expected | length)) and
    ([.layers[].annotations["org.opencontainers.image.title"]] | sort) == ($expected | keys | sort) and
    ([.layers[].annotations["org.opencontainers.image.title"]] | unique | length) == ($expected | length) and
    all(.layers[];
      (. | keys | sort) == (["annotations","digest","mediaType","size"] | sort) and
      (.annotations | keys) == ["org.opencontainers.image.title"] and
      .mediaType == $expected[.annotations["org.opencontainers.image.title"]] and
      (.digest | test("^sha256:[a-f0-9]{64}$")) and
      (.size | type == "number" and . >= 0 and . <= 16777216)) and
    ([.layers[].size] | add) <= 67108864
  ' "$evidence_manifest" >/dev/null || fail "admission evidence OCI manifest binding mismatch"
}

verify_evidence_files() {
  evidence_directory=$1
  evidence_manifest=$2
  evidence_entries | while IFS='|' read -r evidence_name evidence_media_type; do
    evidence_path="$evidence_directory/$evidence_name"
    [ -f "$evidence_path" ] || fail "admission evidence entry is missing"
    expected_digest=$(jq -er --arg name "$evidence_name" '.layers[] |
      select(.annotations["org.opencontainers.image.title"] == $name) | .digest' "$evidence_manifest")
    expected_size=$(jq -er --arg name "$evidence_name" '.layers[] |
      select(.annotations["org.opencontainers.image.title"] == $name) | .size' "$evidence_manifest")
    [ "sha256:$(sha256sum "$evidence_path" | awk '{print $1}')" = "$expected_digest" ] &&
      [ "$(wc -c <"$evidence_path" | tr -d ' ')" = "$expected_size" ] ||
      fail "admission evidence entry descriptor mismatch"
  done
}

restore_evidence_entries() {
  evidence_reference=$1
  evidence_manifest=$2
  evidence_directory=$3
  mkdir -p "$evidence_directory"
  evidence_entries | while IFS='|' read -r evidence_name evidence_media_type; do
    temporary="$evidence_directory/$evidence_name.next"
    regctl artifact get "$evidence_reference" --file "$evidence_name" >"$temporary" 2>/dev/null ||
      fail "admission evidence entry recovery failed"
    mv "$temporary" "$evidence_directory/$evidence_name"
  done
  verify_evidence_files "$evidence_directory" "$evidence_manifest"
}

verify_recovered_evidence() {
  evidence_directory=$1
  evidence_manifest=$2
  expected_manifest_digest=$3
  expected_artifact=$4
  expected_image=$5
  expected_receipt=$6
  expected_policy_revision=$7
  expected_policy_sha256=$8
  required_verdict=$9
  verify_evidence_manifest "$evidence_manifest" "$expected_manifest_digest" "$expected_artifact" "$expected_image" \
    "$expected_policy_revision" "$expected_policy_sha256"
  verify_evidence_files "$evidence_directory" "$evidence_manifest"
  receipt="$evidence_directory/admission.receipt.json"
  signature_binding="$evidence_directory/signature.binding.json"
  [ "$(sha256sum "$receipt" | awk '{print $1}')" = "$expected_receipt" ] ||
    fail "durable admission receipt payload mismatch"
  verdict=$(jq -er '.verdict' "$receipt")
  [ "$required_verdict" = ACCEPTED ] || [ "$required_verdict" = REJECTED ] ||
    fail "required admission verdict is invalid"
  [ "$verdict" = "$required_verdict" ] ||
    fail "durable admission verdict mismatch"
  signature_identity=$(jq -er '.signatureIdentity' "$receipt")
  jq -e --arg artifact "$expected_artifact" --arg image "$expected_image" \
    --arg policy "$expected_policy_revision" --arg policy_sha "$expected_policy_sha256" '
    (. | keys | sort) == (["artifactId","imageDigest","immutableBuildSHA256","policyRevision","policySHA256",
      "provenanceSHA256","sbomSHA256","signatureIdentity","signatureSHA256","specSHA256",
      "version","verdict","vulnerabilityEvidenceSHA256"] | sort) and
    .version == "v1" and .artifactId == $artifact and .imageDigest == $image and
    .policyRevision == $policy and .policySHA256 == $policy_sha and
    (.specSHA256 | test("^[a-f0-9]{64}$")) and (.immutableBuildSHA256 | test("^[a-f0-9]{64}$")) and
    (.provenanceSHA256 | test("^[a-f0-9]{64}$")) and (.sbomSHA256 | test("^[a-f0-9]{64}$")) and
    (.vulnerabilityEvidenceSHA256 | test("^[a-f0-9]{64}$")) and
    (.signatureSHA256 | test("^[a-f0-9]{64}$")) and (.verdict == "ACCEPTED" or .verdict == "REJECTED")
  ' "$receipt" >/dev/null || fail "durable admission receipt binding mismatch"
  jq -e --arg image "$expected_image" --arg policy "$expected_policy_revision" \
    --arg policy_sha "$expected_policy_sha256" --arg verdict "$verdict" --arg identity "$signature_identity" '
    (. | keys | sort) == (["imageDigest","policyRevision","policySHA256","signatureIdentity","verdict",
      "verification","version"] | sort) and
    .version == "v1" and .imageDigest == $image and .policyRevision == $policy and
    .policySHA256 == $policy_sha and .signatureIdentity == $identity and .verdict == $verdict and
    .verification == "cosign-key-v1"
  ' "$signature_binding" >/dev/null || fail "durable signature binding mismatch"
  [ "$(sha256sum "$signature_binding" | awk '{print $1}')" = "$(jq -er .signatureSHA256 "$receipt")" ] &&
    [ "$(sha256sum "$evidence_directory/provenance.json" | awk '{print $1}')" = "$(jq -er .provenanceSHA256 "$receipt")" ] &&
    [ "$(sha256sum "$evidence_directory/sbom.json" | awk '{print $1}')" = "$(jq -er .sbomSHA256 "$receipt")" ] &&
    [ "$(sha256sum "$evidence_directory/vulnerability.json" | awk '{print $1}')" = "$(jq -er .vulnerabilityEvidenceSHA256 "$receipt")" ] ||
    fail "durable admission evidence hash mismatch"
  jq -e --arg image "$expected_image" --argjson policy "$expected_policy_revision" \
    --arg policy_sha "$expected_policy_sha256" '
    .schema == "kodex.dev/image-provenance-binding/v1" and .manifestDigest == $image and
    .policyRevision == $policy and .policySHA256 == $policy_sha
  ' "$evidence_directory/provenance.json" >/dev/null || fail "durable provenance binding mismatch"
  jq -e 'type == "array" and length > 0' "$evidence_directory/native-provenance.json" >/dev/null ||
    fail "durable native provenance is invalid"
  jq -e 'type == "object"' "$evidence_directory/sbom.json" >/dev/null || fail "durable SBOM is invalid"
  jq -e 'type == "object"' "$evidence_directory/vulnerability.json" >/dev/null ||
    fail "durable vulnerability evidence is invalid"
  expected_subject="$evidence_directory/.expected-image-digest.$$"
  printf '%s\n' "$expected_image" >"$expected_subject"
  cmp -s "$expected_subject" "$evidence_directory/image-digest.subject" ||
    fail "durable image digest subject mismatch"
  rm -f "$expected_subject"
  if [ "$verdict" = ACCEPTED ]; then
    echo "$signature_identity" | grep -Eq '^[a-f0-9]{64}$' || fail "durable signature identity is invalid"
    [ "$(sha256sum "$evidence_directory/cosign.pub" | awk '{print $1}')" = "$signature_identity" ] ||
      fail "durable signature identity mismatch"
    for signed_name in image-digest provenance native-provenance sbom vulnerability; do
      signed_file="$evidence_directory/$signed_name.json"
      [ "$signed_name" = image-digest ] && signed_file="$evidence_directory/image-digest.subject"
      cosign verify-blob --key "$evidence_directory/cosign.pub" \
        --signature "$evidence_directory/$signed_name.sig" "$signed_file" >/dev/null 2>&1 ||
        fail "durable evidence signature verification failed"
    done
  else
    [ "$signature_identity" = not-applicable-rejected ] || fail "rejected evidence signature identity mismatch"
    for signature_file in "$evidence_directory"/*.sig; do
      [ ! -s "$signature_file" ] || fail "rejected evidence contains a signature"
    done
  fi
}

publish_or_verify_evidence() {
  evidence_tag=$1
  evidence_manifest=$2
  evidence_type=application/vnd.kodex.image-admission-evidence.v2
  config_type=application/vnd.kodex.image-admission-evidence.config.v2+json
  printf '{}' >/work/evidence.config.json
  if ! regctl manifest get "$evidence_tag" --format raw-body >"$evidence_manifest" 2>/dev/null; then
    set -- --artifact-type "$evidence_type" --config-type "$config_type" \
      --config-file /work/evidence.config.json --file-title --strip-dirs \
      --annotation "kodex.dev/evidence-schema=kodex.dev/image-admission-evidence/v2" \
      --annotation "kodex.dev/artifact-id=$artifact_id" \
      --annotation "kodex.dev/image-digest=$image_digest" \
      --annotation "kodex.dev/policy-revision=$POLICY_REVISION" \
      --annotation "kodex.dev/policy-sha256=$POLICY_SHA256"
    while IFS='|' read -r evidence_name evidence_media_type; do
      set -- "$@" --file-media-type "$evidence_media_type" --file "/work/$evidence_name"
    done <<EOF
$(evidence_entries)
EOF
    regctl artifact put "$@" "$evidence_tag" >/dev/null
  fi
  regctl manifest get "$evidence_tag" --format raw-body >"$evidence_manifest"
  evidence_manifest_digest="sha256:$(sha256sum "$evidence_manifest" | awk '{print $1}')"
  verify_evidence_manifest "$evidence_manifest" "$evidence_manifest_digest" "$artifact_id" "$image_digest" \
    "$POLICY_REVISION" "$POLICY_SHA256"
  restore_evidence_entries "$evidence_tag" "$evidence_manifest" /work/evidence.readback
  evidence_entries | while IFS='|' read -r evidence_name evidence_media_type; do
    cmp -s "/work/$evidence_name" "/work/evidence.readback/$evidence_name" ||
      fail "immutable admission evidence tag already points to another payload"
  done
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
  regctl registry set "$host" --skip-check --tls enabled \
    --cacert "$(cat /identity/ca.pem)" \
    --client-cert "$(cat /identity/registry-client.crt)" \
    --client-key "$(cat /identity/registry-client.key)"
  regctl registry login "$host" --skip-check \
    --user "$(tr -d '\r\n' <"$username_file")" \
    --pass-stdin <"$password_file" >/dev/null
}

write_syft_registry_config() {
  host=$1
  username_file=$2
  password_file=$3
  jq -n --arg authority "$host" --rawfile username "$username_file" \
    --rawfile password "$password_file" '
      {
        "check-for-app-update": false,
        parallelism: 1,
        registry: {
          "ca-cert": "/identity/ca.pem",
          auth: [{
            authority: $authority,
            username: ($username | rtrimstr("\n") | rtrimstr("\r")),
            password: ($password | rtrimstr("\n") | rtrimstr("\r")),
            "tls-cert": "/identity/registry-client.crt",
            "tls-key": "/identity/registry-client.key"
          }]
        }
      }
    ' >/tmp/syft.json || fail "Syft registry configuration failed"
  chmod 0600 /tmp/syft.json
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
    .digest' \
    /work/image-index.json >/work/platform-manifests
  : >/work/native-provenance.jsonl
  while IFS= read -r platform_digest; do
    echo "$platform_digest" | grep -Eq '^sha256:[a-f0-9]{64}$' || fail "platform manifest digest is invalid"
    platform_ref="${subject_name}@${platform_digest}"
    regctl image inspect "$platform_ref" --format '{{json .Config.Labels}}' >/work/labels.json
    regctl image inspect "$platform_ref" --format '{{json .Config}}' >/work/image-config.json
    jq -e --arg spec "$spec_sha256" --arg immutable "$immutable_build_sha256" \
      --arg source "$source_sha256" --arg context "$context_sha256" --arg base "$base_image_digest" \
      --arg builder "$builder_sha256" --arg frontend "$frontend_sha256" --arg toolchain "$toolchain_sha256" \
      --arg policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" --arg runtime "$runtime_contract_sha256" '
      ."kodex.dev/spec-sha256" == $spec and
      ."kodex.dev/immutable-build-sha256" == $immutable and
      ."kodex.dev/source-sha256" == $source and
      ."kodex.dev/context-sha256" == $context and
      ."kodex.dev/base-image-digest" == $base and
      ."kodex.dev/builder-sha256" == $builder and
      ."kodex.dev/frontend-sha256" == $frontend and
      ."kodex.dev/toolchain-sha256" == $toolchain and
      ."kodex.dev/policy-revision" == $policy and
      ."kodex.dev/policy-sha256" == $policy_sha and
      ."kodex.dev/runtime-contract-sha256" == $runtime
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
      --arg build_type "$EXPECTED_BUILD_TYPE" -f /opt/kodex/provenance-policy.jq \
      /work/provenance.statement.json >/dev/null || fail "native provenance binding mismatch"
    jq -c . /work/provenance.statement.json >>/work/native-provenance.jsonl
  done </work/platform-manifests
  jq -sSjc . /work/native-provenance.jsonl >/work/native-provenance.json
  jq -Sjc -n --arg build_type "$EXPECTED_BUILD_TYPE" --arg builder_id "$EXPECTED_BUILDER_ID" \
    --arg immutable "$immutable_build_sha256" --arg manifest "$image_digest" \
    --argjson policy "$POLICY_REVISION" --arg policy_sha "$POLICY_SHA256" \
    --arg schema "kodex.dev/image-provenance-binding/v1" --arg spec "$spec_sha256" \
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

if [ "${1:-}" = validate-evidence-recovery ]; then
  [ "$#" -eq 11 ] || fail "evidence recovery fixture is invalid"
  artifact_id=$2
  image_digest=$3
  promotion_receipt=$4
  evidence_reference=$5
  evidence_manifest=$6
  expected_manifest_digest=$7
  expected_policy_revision=$8
  expected_policy_sha256=$9
  required_verdict=${10}
  evidence_directory=${11}
  verify_evidence_manifest "$evidence_manifest" "$expected_manifest_digest" "$artifact_id" "$image_digest" \
    "$expected_policy_revision" "$expected_policy_sha256"
  restore_evidence_entries "$evidence_reference" "$evidence_manifest" "$evidence_directory"
  verify_recovered_evidence "$evidence_directory" "$evidence_manifest" "$expected_manifest_digest" \
    "$artifact_id" "$image_digest" "$promotion_receipt" "$expected_policy_revision" \
    "$expected_policy_sha256" "$required_verdict"
  exit 0
fi

if [ "${1:-}" = validate-evidence ]; then
  [ "$#" -eq 10 ] || fail "evidence fixture is invalid"
  artifact_id=$2
  image_digest=$3
  promotion_receipt=$4
  evidence_directory=$5
  evidence_manifest=$6
  expected_manifest_digest=$7
  expected_policy_revision=$8
  expected_policy_sha256=$9
  required_verdict=${10}
  verify_recovered_evidence "$evidence_directory" "$evidence_manifest" "$expected_manifest_digest" \
    "$artifact_id" "$image_digest" "$promotion_receipt" "$expected_policy_revision" \
    "$expected_policy_sha256" "$required_verdict"
  exit 0
fi

require_policy

case "${1:-}" in
  claim)
    claim_admission
    write_marker claim.complete
    ;;
  scan)
    wait_for_marker claim.complete
    load_owner_claim
    login_registry "$staging_host" /identity/username /identity/password
    [ "$(regctl image digest "$source_ref")" = "$image_digest" ] || fail "staging digest mismatch"
    verify_image_and_provenance
    write_syft_registry_config "$staging_host" /identity/username /identity/password
    syft --config /tmp/syft.json --from registry "$source_ref" \
      -o spdx-json=/work/sbom.json || fail "SBOM generation failed"
    if GRYPE_CHECK_FOR_APP_UPDATE=false \
      grype sbom:/work/sbom.json --fail-on high -o json >/work/vulnerability.json; then
      printf '%s\n' ACCEPTED >/work/verdict
    else
      jq -e '.matches | type == "array"' /work/vulnerability.json >/dev/null ||
        fail "vulnerability scan failed"
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
      COSIGN_PASSWORD=$(cat /identity/cosign.password)
      export COSIGN_PASSWORD
      printf '%s\n' "$image_digest" >/work/image-digest.subject
      cosign sign-blob --yes --key /identity/cosign.key --output-signature /work/image-digest.sig /work/image-digest.subject
      for evidence in provenance native-provenance sbom vulnerability; do
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
      for evidence in provenance native-provenance sbom vulnerability; do
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
    printf '%s\n' "$image_digest" >/work/image-digest.subject
    cp /identity/cosign.pub /work/cosign.pub
    for signature in image-digest provenance native-provenance sbom vulnerability; do
      [ -f "/work/$signature.sig" ] || : >"/work/$signature.sig"
    done
    evidence_total=0
    while IFS='|' read -r evidence_file evidence_media_type; do
      evidence_size=$(wc -c <"/work/$evidence_file" | tr -d ' ')
      [ "$evidence_size" -le 16777216 ] || fail "admission evidence entry exceeds bound"
      evidence_total=$((evidence_total + evidence_size))
      [ "$evidence_total" -le 67108864 ] || fail "admission evidence exceeds bound"
    done <<EOF
$(evidence_entries)
EOF
    evidence_host=${EVIDENCE_REPOSITORY%%/*}
    evidence_tag="${EVIDENCE_REPOSITORY}:artifact-${artifact_id}"
    login_registry "$evidence_host" /identity/evidence.username /identity/evidence.password
    publish_or_verify_evidence "$evidence_tag" /work/admission.evidence-manifest.json
    evidence_manifest_digest="sha256:$(sha256sum /work/admission.evidence-manifest.json | awk '{print $1}')"
    verify_recovered_evidence /work/evidence.readback /work/admission.evidence-manifest.json \
      "$evidence_manifest_digest" "$artifact_id" "$image_digest" \
      "$(cat /work/admission.receipt.sha256)" "$POLICY_REVISION" "$POLICY_SHA256" "$verdict"
    printf '%s\n' "$evidence_manifest_digest" >/work/admission.receipt-manifest.digest
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
    promoted_evidence_tag="${PROMOTION_EVIDENCE_REPOSITORY}:artifact-${artifact_id}"
    promotion_reference="${PROMOTION_REPOSITORY}@${image_digest}"
    promoted_reference="${PROMOTED_PULL_REPOSITORY}@${image_digest}"
    evidence_host=${EVIDENCE_REPOSITORY%%/*}
    evidence_reference="${EVIDENCE_REPOSITORY}@${staging_receipt_manifest_digest}"
    login_registry "$evidence_host" /identity/evidence.username /identity/evidence.password
    regctl manifest get "$evidence_reference" --format raw-body >/work/admission.evidence-manifest.json
    verify_evidence_manifest /work/admission.evidence-manifest.json "$staging_receipt_manifest_digest" \
      "$artifact_id" "$image_digest" "$POLICY_REVISION" "$POLICY_SHA256"
    restore_evidence_entries "$evidence_reference" /work/admission.evidence-manifest.json /work/evidence
    verify_recovered_evidence /work/evidence /work/admission.evidence-manifest.json \
      "$staging_receipt_manifest_digest" "$artifact_id" "$image_digest" "$promotion_receipt" \
      "$POLICY_REVISION" "$POLICY_SHA256" ACCEPTED
    login_registry "$staging_host" /identity/staging.username /identity/staging.password
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
    if promoted_evidence_digest=$(regctl image digest "$promoted_evidence_tag" 2>/dev/null); then
      [ "$promoted_evidence_digest" = "$staging_receipt_manifest_digest" ] ||
        fail "immutable promoted evidence tag already points to another manifest"
    else
      regctl image copy "$evidence_reference" "$promoted_evidence_tag"
    fi
    regctl manifest get "${PROMOTION_EVIDENCE_REPOSITORY}@${staging_receipt_manifest_digest}" \
      --format raw-body >/work/promoted.admission.evidence-manifest.json
    image_manifest_sha256=$(sha256sum /work/promotion.image-manifest.json | awk '{print $1}')
    promoted_evidence_manifest_digest="sha256:$(sha256sum /work/promoted.admission.evidence-manifest.json | awk '{print $1}')"
    [ "$promoted_evidence_manifest_digest" = "$staging_receipt_manifest_digest" ] ||
      fail "promoted admission evidence manifest digest mismatch"
    jq -Sjc -n --arg image "$image_digest" --arg image_manifest "$image_manifest_sha256" \
      --arg receipt "$promotion_receipt" --arg staging_receipt_manifest "$staging_receipt_manifest_digest" \
      --arg promoted_evidence_manifest "$promoted_evidence_manifest_digest" \
      '{imageManifestDigest:$image,imageManifestSHA256:$image_manifest,
        admissionReceiptSHA256:$receipt,stagingAdmissionReceiptManifestDigest:$staging_receipt_manifest,
        promotedAdmissionEvidenceManifestDigest:$promoted_evidence_manifest}' \
      >/work/promotion.readback.json
    sha256sum /work/promotion.readback.json | awk '{print $1}' >/work/promotion.readback.sha256
    IMAGE_OWNER_PROMOTED_REFERENCE="$promoted_reference" \
    IMAGE_OWNER_PROMOTION_READBACK_SHA256_FILE=/work/promotion.readback.sha256 \
      image-admission-bridge complete
    printf 'promoted image digest: %s\n' "$image_digest"
    ;;
  *) fail "unknown admission phase" ;;
esac
