-- name: role_image_managed__record :exec
WITH inserted AS (
 INSERT INTO control_plane.managed_role_image_recipes(configuration_set_id,organization_id,recipe_id,origin)
 SELECT $1::uuid,$2::uuid,recipe.id,'MANAGED' FROM control_plane.role_image_recipes recipe WHERE recipe.organization_id=$2::uuid AND recipe.ref=$3
 ON CONFLICT(configuration_set_id) DO NOTHING
), revision AS (
 INSERT INTO control_plane.managed_role_image_revisions(configuration_revision_id,configuration_set_id,recipe_generation,recipe_version)
 SELECT candidate.id,$1::uuid,$5,$6 FROM control_plane.managed_configuration_revisions candidate
 WHERE candidate.organization_id=$2::uuid AND candidate.configuration_set_id=$1::uuid AND candidate.ref=$4
 RETURNING configuration_revision_id
)
INSERT INTO control_plane.managed_role_image_builds(build_id,configuration_revision_id)
SELECT build.id,revision.configuration_revision_id FROM control_plane.image_builds build CROSS JOIN revision
WHERE build.organization_id=$2::uuid AND build.ref=$7;
