-- name: role_images_insert_promoted_revision :one
INSERT INTO control_plane.role_image_recipe_revisions
    (ref, organization_id, project_id, recipe_id, revision, recipe_version,
     recipe_generation, specification, spec_sha256, image_artifact_id,
     provenance_sha256, source_sha256, immutable_build_sha256, manifest_digest,
     promoted_reference, promotion_receipt_sha256, created_by)
SELECT @revision_ref, request.organization_id, request.project_id, request.recipe_id,
       COALESCE((SELECT max(existing.revision) + 1
                 FROM control_plane.role_image_recipe_revisions existing
                 WHERE existing.recipe_id = request.recipe_id), 1),
       artifact.recipe_version, artifact.recipe_generation, artifact.specification,
       artifact.spec_sha256, artifact.id, artifact.provenance_sha256,
       @source_sha256, artifact.immutable_build_sha256, artifact.manifest_digest,
       artifact.promoted_reference, artifact.promotion_readback_sha256, request.requested_by
FROM control_plane.role_image_promotion_requests request
JOIN control_plane.image_artifacts artifact ON artifact.id = request.image_artifact_id
JOIN control_plane.role_image_recipes recipe ON recipe.id = request.recipe_id
WHERE request.organization_id = @organization_id::uuid
  AND request.id = @promotion_request_id::uuid
  AND request.state = 'PROMOTED'
	AND request.organization_id = artifact.organization_id
	AND request.project_id = artifact.project_id
	AND request.recipe_id = artifact.recipe_id
  AND artifact.id = @image_artifact_id::uuid
  AND artifact.promotion_state = 'PROMOTED'
	AND artifact.admission_state = 'ACCEPTED'
	AND artifact.admission_verdict = 'ACCEPTED'
	AND artifact.admission_receipt_sha256 ~ '^[a-f0-9]{64}$'
	AND artifact.admission_receipt_oci_manifest_digest ~ '^sha256:[a-f0-9]{64}$'
	AND artifact.promotion_readback_sha256 = @promotion_readback_sha256
  AND artifact.provenance_sha256 = request.expected_provenance_sha256
  AND artifact.manifest_digest = request.manifest_digest
	AND artifact.specification->>'SourceSHA256' = @source_sha256
	AND recipe.organization_id = request.organization_id
	AND recipe.project_id = request.project_id
  AND recipe.active_image_artifact_id = artifact.id
	AND recipe.version = artifact.recipe_version + 1
	AND recipe.generation = artifact.recipe_generation
	AND recipe.spec_sha256 = artifact.spec_sha256
RETURNING ref, revision, recipe_version, recipe_generation, created_at;
