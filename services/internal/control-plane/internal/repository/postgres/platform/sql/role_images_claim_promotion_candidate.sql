-- name: role_images_claim_promotion_candidate :one
SELECT artifact.id::text, artifact.ref, artifact.version, artifact.promotion_fence
FROM control_plane.image_artifacts artifact
JOIN control_plane.role_image_promotion_requests request
  ON request.id = artifact.promotion_request_id
JOIN control_plane.role_image_recipes recipe
  ON recipe.id = artifact.recipe_id
WHERE artifact.organization_id = $1::uuid
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.admission_verdict = 'ACCEPTED'
	AND artifact.promoted_reference = ''
	AND artifact.manifest_digest ~ '^sha256:[a-f0-9]{64}$'
	AND artifact.provenance_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.immutable_build_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.sbom_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.vulnerability_evidence_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.signature_identity <> ''
	AND artifact.signature_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.admission_revision > 0
	AND artifact.admission_receipt_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.admission_receipt_oci_manifest_digest ~ '^sha256:[a-f0-9]{64}$'
  AND request.organization_id = artifact.organization_id
  AND request.image_artifact_id = artifact.id
  AND request.expected_provenance_sha256 = artifact.provenance_sha256
  AND request.manifest_digest = artifact.manifest_digest
	AND request.receipt_sha256 ~ '^[a-f0-9]{64}$'
	AND recipe.organization_id = artifact.organization_id
	AND recipe.project_id = artifact.project_id
	AND recipe.state = 'ACTIVE'
	AND recipe.version = artifact.recipe_version
	AND recipe.generation = artifact.recipe_generation
	AND recipe.spec_sha256 = artifact.spec_sha256
	AND recipe.policy_revision = artifact.policy_revision
	AND recipe.policy_sha256 = artifact.policy_sha256
	AND recipe.role_runtime_contract_revision = artifact.role_runtime_contract_revision
	AND recipe.role_runtime_contract_sha256 = artifact.role_runtime_contract_sha256
  AND (
      (request.state = 'QUEUED' AND artifact.promotion_state = 'PENDING')
      OR (request.state = 'PROMOTING' AND artifact.promotion_state = 'CLAIMED'
          AND artifact.promotion_claim_expires_at <= clock_timestamp())
      OR (request.state = 'PROMOTING' AND artifact.promotion_state = 'AUTHORIZED'
          AND artifact.promotion_authorization_expires_at <= clock_timestamp())
  )
ORDER BY artifact.created_at, artifact.ref
FOR UPDATE OF artifact, request, recipe SKIP LOCKED
LIMIT 1
