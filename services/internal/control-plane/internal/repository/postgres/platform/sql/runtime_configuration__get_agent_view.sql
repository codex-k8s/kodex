-- name: runtime_configuration__get_agent_view :one
SELECT config.ref,
       config.version_number,
       agent.ref,
       config.runtime_profile_key,
       config.provider,
       config.model,
       config.digest,
       config.created_at,
       policy.ref,
       policy.version_number,
       policy.mode,
       policy.account_candidates,
       policy.digest,
       policy.created_at,
       published.ref,
       published.version_number,
       published.state,
       published.content,
       published.digest,
       published.validation_errors,
       published.created_at,
       published.published_at,
       draft.ref,
       draft.version_number,
       draft.state,
       draft.content,
       draft.digest,
       draft.validation_errors,
       draft.created_at,
       draft.published_at,
       binding.ref,
       binding.version,
       binding.digest,
       environment.ref,
       environment.version,
       COALESCE(project.ref, ''),
       environment.name,
       environment.description,
       environment.state,
       environment.updated_at,
       environment_version.ref,
       environment_version.version_number,
       environment_version.non_secret_values,
       environment_version.secret_descriptors,
       COALESCE(image_artifact.ref, ''),
       COALESCE(image_recipe.ref, ''),
       COALESCE(image_artifact.recipe_generation, 0),
       COALESCE(image_artifact.promoted_reference, ''),
       COALESCE(image_artifact.manifest_digest, ''),
       COALESCE(image_artifact.role_runtime_contract_revision, 0),
       COALESCE(image_artifact.role_runtime_contract_sha256, ''),
       environment_version.selected_tools,
       environment_version.core_digest,
       environment_version.resource_policy,
       environment_version.volume_policy,
       environment_version.network_policy,
       environment_version.kubernetes_access_profile,
       environment_version.resources_digest,
       environment_version.volumes_digest,
       environment_version.network_digest,
       environment_version.rbac_digest,
       environment_version.digest,
       environment_version.created_at,
       agent.version
FROM control_plane.agents agent
LEFT JOIN control_plane.projects project ON project.id = agent.project_id
JOIN control_plane.agent_runtime_config_versions config ON config.id = agent.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
JOIN control_plane.agent_config_overlay_versions published ON published.id = agent.current_config_overlay_id
LEFT JOIN LATERAL (
    SELECT candidate.*
    FROM control_plane.agent_config_overlay_versions candidate
    WHERE candidate.agent_id = agent.id AND candidate.state IN ('DRAFT', 'VALID', 'INVALID')
    ORDER BY candidate.version_number DESC
    LIMIT 1
) draft ON true
JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id = agent.id
JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
JOIN control_plane.runtime_environment_versions environment_version ON environment_version.id = environment.current_version_id
LEFT JOIN control_plane.image_artifacts image_artifact ON image_artifact.id = environment_version.role_image_artifact_id
LEFT JOIN control_plane.role_image_recipes image_recipe ON image_recipe.id = image_artifact.recipe_id
WHERE agent.organization_id = $1::uuid
  AND agent.ref = $2
  AND (agent.system_key = 'system-assistant' OR $3 IN ('OWNER', 'ADMINISTRATOR') OR EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      WHERE membership.project_id = agent.project_id
        AND membership.subject_id = $4::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ));
