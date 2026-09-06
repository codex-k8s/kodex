-- name: role_image_impact__target :one
SELECT artifact.id::text, recipe.ref, artifact.recipe_generation, build.ref,
       artifact.ref, artifact.manifest_digest, artifact.policy_sha256
FROM control_plane.managed_configuration_revisions revision
JOIN control_plane.managed_role_image_revisions mapping ON mapping.configuration_revision_id=revision.id
JOIN control_plane.managed_role_image_recipes owner ON owner.configuration_set_id=mapping.configuration_set_id
 AND owner.organization_id=revision.organization_id
JOIN control_plane.role_image_recipes recipe ON recipe.id=owner.recipe_id AND recipe.organization_id=owner.organization_id
JOIN control_plane.managed_role_image_builds mapped_build ON mapped_build.configuration_revision_id=revision.id
JOIN control_plane.image_builds build ON build.id=mapped_build.build_id AND build.organization_id=recipe.organization_id
 AND build.recipe_id=recipe.id AND build.recipe_generation=mapping.recipe_generation
JOIN control_plane.image_artifacts artifact ON artifact.build_id=build.id AND artifact.organization_id=recipe.organization_id
 AND artifact.recipe_id=recipe.id AND artifact.recipe_generation=mapping.recipe_generation
JOIN control_plane.projects project ON project.id=recipe.project_id AND project.organization_id=recipe.organization_id
WHERE revision.organization_id=@organization_id::uuid AND revision.configuration_set_id=@configuration_id::uuid
 AND revision.ref=@revision_ref AND revision.published_at IS NOT NULL
 AND recipe.state='ACTIVE' AND project.lifecycle='ACTIVE'
 AND artifact.admission_state='ACCEPTED' AND artifact.promotion_state='PROMOTED'
 AND artifact.promoted_reference<>'' AND artifact.promoted_at IS NOT NULL
 AND artifact.admission_receipt_sha256<>'' AND artifact.promotion_readback_sha256<>''
 AND artifact.policy_revision=@policy_revision AND artifact.policy_sha256=@policy_digest
 AND artifact.role_runtime_contract_revision=@contract_revision AND artifact.role_runtime_contract_sha256=@contract_digest
ORDER BY artifact.promoted_at DESC, artifact.ref COLLATE "C" LIMIT 1
FOR SHARE OF artifact,recipe,build;
