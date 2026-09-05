-- name: role_image_managed__lineage :one
SELECT configuration.ref, revision.ref, revision.revision, configuration.managed_by,
       configuration.source, configuration.source_revision, owner.origin
FROM control_plane.role_image_recipes recipe
JOIN control_plane.managed_role_image_recipes owner ON owner.recipe_id = recipe.id AND owner.organization_id = recipe.organization_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = owner.configuration_set_id AND configuration.organization_id = owner.organization_id
JOIN control_plane.managed_role_image_revisions mapping ON mapping.configuration_set_id = configuration.id AND mapping.recipe_generation = recipe.generation
JOIN control_plane.managed_configuration_revisions revision ON revision.id = mapping.configuration_revision_id AND revision.organization_id = recipe.organization_id
WHERE recipe.organization_id = $1::uuid AND recipe.ref = $2;
