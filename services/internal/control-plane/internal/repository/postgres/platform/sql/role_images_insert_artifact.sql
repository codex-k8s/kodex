-- name: platform__role_images_insert_artifact :one
INSERT INTO control_plane.image_artifacts
    (ref, organization_id, project_id, recipe_id, recipe_version, recipe_generation,
     spec_sha256, build_id, build_version, build_attempt, specification,
     policy_revision, policy_sha256, role_runtime_contract_revision,
     role_runtime_contract_sha256, staging_reference, manifest_digest,
     immutable_build_sha256, provenance_sha256)
VALUES
    ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8::uuid, $9, $10, $11,
     $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING id::text
