-- name: role_image_managed__build :exec
INSERT INTO control_plane.managed_role_image_builds(build_id, configuration_revision_id)
SELECT build.id, revision.configuration_revision_id
FROM control_plane.image_builds build
JOIN control_plane.managed_role_image_recipes mapping ON mapping.recipe_id = build.recipe_id AND mapping.organization_id = build.organization_id
JOIN control_plane.managed_role_image_revisions revision ON revision.configuration_set_id = mapping.configuration_set_id AND revision.recipe_generation = build.recipe_generation
WHERE build.organization_id = $1::uuid AND build.ref = $2
ON CONFLICT (build_id) DO NOTHING;
