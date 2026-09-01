-- name: role_images_link_promotion_request :one
UPDATE control_plane.image_artifacts
SET promotion_request_id = @promotion_request_id::uuid,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND id = @image_artifact_id::uuid
  AND recipe_id = @recipe_id::uuid
  AND version = @expected_version
  AND recipe_version = @recipe_version
  AND recipe_generation = @recipe_generation
  AND spec_sha256 = @spec_sha256
  AND provenance_sha256 = @expected_provenance_sha256
  AND manifest_digest = @manifest_digest
	AND immutable_build_sha256 = @immutable_build_sha256
	AND sbom_sha256 = @sbom_sha256
	AND vulnerability_evidence_sha256 = @vulnerability_evidence_sha256
	AND signature_identity = @signature_identity
	AND signature_sha256 = @signature_sha256
	AND admission_revision = @admission_revision
	AND admission_receipt_sha256 = @admission_receipt_sha256
	AND admission_receipt_oci_manifest_digest = @admission_receipt_oci_manifest_digest
	AND policy_revision = @policy_revision
	AND policy_sha256 = @policy_sha256
	AND role_runtime_contract_revision = @role_runtime_contract_revision
	AND role_runtime_contract_sha256 = @role_runtime_contract_sha256
  AND admission_state = 'ACCEPTED'
  AND admission_verdict = 'ACCEPTED'
  AND promotion_state = 'PENDING'
  AND promotion_request_id IS NULL
RETURNING version, updated_at;
