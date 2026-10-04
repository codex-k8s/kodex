SELECT revision.state,revision.published_at IS NOT NULL,recipe.state,recipe.scope_kind,
 count(build.id),count(artifact.id),
 COALESCE(bool_or(artifact.admission_state='ACCEPTED' AND artifact.promotion_state='PROMOTED'),false),
 COALESCE(bool_or(artifact.policy_revision=$3 AND artifact.policy_sha256=$4),false),
 COALESCE(bool_or(artifact.role_runtime_contract_revision=$5 AND artifact.role_runtime_contract_sha256=$6),false)
FROM control_plane.managed_configuration_sets configuration
JOIN control_plane.managed_configuration_revisions revision ON revision.configuration_set_id=configuration.id
JOIN control_plane.managed_role_image_recipes owner ON owner.configuration_set_id=configuration.id
JOIN control_plane.role_image_recipes recipe ON recipe.id=owner.recipe_id
LEFT JOIN control_plane.managed_role_image_revisions mapping ON mapping.configuration_revision_id=revision.id
LEFT JOIN control_plane.managed_role_image_builds mapped_build ON mapped_build.configuration_revision_id=revision.id
LEFT JOIN control_plane.image_builds build ON build.id=mapped_build.build_id AND build.recipe_generation=mapping.recipe_generation
LEFT JOIN control_plane.image_artifacts artifact ON artifact.build_id=build.id AND artifact.recipe_generation=mapping.recipe_generation
WHERE configuration.organization_id=$1::uuid AND configuration.ref=$2
GROUP BY revision.state,revision.published_at,recipe.state,recipe.scope_kind;
