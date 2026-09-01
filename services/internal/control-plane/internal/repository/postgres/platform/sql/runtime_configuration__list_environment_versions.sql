-- name: runtime_configuration__list_environment_versions :many
SELECT version.ref,
       version.version_number,
       version.non_secret_values,
       version.secret_descriptors,
       COALESCE(image_artifact.ref, ''),
       COALESCE(image_recipe.ref, ''),
       COALESCE(image_artifact.recipe_generation, 0),
       COALESCE(image_artifact.promoted_reference, ''),
       COALESCE(image_artifact.manifest_digest, ''),
       version.selected_tools,
       version.core_digest,
       version.resource_policy,
       version.volume_policy,
       version.network_policy,
       version.kubernetes_access_profile,
       version.resources_digest,
       version.volumes_digest,
       version.network_digest,
       version.rbac_digest,
       version.digest,
       version.created_at
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.runtime_environment_versions version ON version.environment_set_id = environment.id
LEFT JOIN control_plane.image_artifacts image_artifact ON image_artifact.id = version.role_image_artifact_id
LEFT JOIN control_plane.role_image_recipes image_recipe ON image_recipe.id = image_artifact.recipe_id
WHERE environment.organization_id = @organization_id::uuid
  AND environment.ref = @environment_ref
  AND (@before_version::bigint = 0 OR version.version_number < @before_version::bigint)
  AND (@platform_role IN ('OWNER', 'ADMINISTRATOR') OR EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      WHERE membership.project_id = environment.project_id
        AND membership.subject_id = @actor_id::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ))
ORDER BY version.version_number DESC
LIMIT @page_size;
