-- name: runtime_configuration__get_environment_lifecycle_snapshot :one
SELECT environment.ref,
       environment.version,
       project.ref,
       environment.name,
       environment.description,
       environment.state,
       environment.updated_at,
       current_version.ref,
       current_version.version_number,
       current_version.non_secret_values,
       current_version.secret_descriptors,
       COALESCE(image_artifact.ref, ''),
       COALESCE(image_recipe.ref, ''),
       COALESCE(image_artifact.recipe_generation, 0),
       COALESCE(image_artifact.promoted_reference, ''),
       COALESCE(image_artifact.manifest_digest, ''),
       current_version.selected_tools,
       current_version.core_digest,
       current_version.resource_policy,
       current_version.volume_policy,
       current_version.network_policy,
       current_version.kubernetes_access_profile,
       current_version.resources_digest,
       current_version.volumes_digest,
       current_version.network_digest,
       current_version.rbac_digest,
       current_version.digest,
       current_version.created_at
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id
JOIN control_plane.runtime_environment_versions current_version ON current_version.id = environment.current_version_id
LEFT JOIN control_plane.image_artifacts image_artifact ON image_artifact.id = current_version.role_image_artifact_id
LEFT JOIN control_plane.role_image_recipes image_recipe ON image_recipe.id = image_artifact.recipe_id
WHERE environment.organization_id = @organization_id::uuid
  AND environment.ref = @environment_ref
  AND environment.state <> 'DELETED';
