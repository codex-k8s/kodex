-- name: role_image_managed_readback :one
SELECT configuration.ref, revision.ref, revision.state, revision.content,
       mapping.recipe_generation, build.ref
FROM control_plane.role_image_recipes recipe
JOIN control_plane.managed_role_image_recipes owner ON owner.recipe_id = recipe.id AND owner.organization_id = recipe.organization_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = owner.configuration_set_id
JOIN control_plane.managed_role_image_revisions mapping ON mapping.configuration_set_id = configuration.id AND mapping.recipe_generation = recipe.generation
JOIN control_plane.managed_configuration_revisions revision ON revision.id = mapping.configuration_revision_id AND revision.id = configuration.current_revision_id
JOIN control_plane.managed_role_image_builds mapped_build ON mapped_build.configuration_revision_id = revision.id
JOIN control_plane.image_builds build ON build.id = mapped_build.build_id
WHERE recipe.ref = $1 AND build.ref = $2;
