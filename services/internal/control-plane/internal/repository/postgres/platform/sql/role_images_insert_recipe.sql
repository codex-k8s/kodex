-- name: role_images_insert_recipe :one
INSERT INTO control_plane.role_image_recipes
    (ref, organization_id, project_id, role_definition_id, name, state, specification,
     generation, spec_sha256, policy_revision, policy_sha256,
     role_runtime_contract_revision, role_runtime_contract_sha256, created_by)
VALUES
    ($1, $2::uuid, $3::uuid, $4::uuid, $5, 'ACTIVE', $6, 1, $7, $8, $9, $10, $11, $12::uuid)
RETURNING id::text
