-- name: runtime_overlay__selected_image :one
SELECT artifact.ref, artifact.manifest_digest
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id = agent.id
JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
JOIN control_plane.runtime_environment_versions version ON version.id =
    CASE WHEN agent.project_id IS NULL AND agent.system_key = 'system-assistant' AND binding.environment_version_id IS NULL
         THEN environment.current_version_id ELSE binding.environment_version_id END
JOIN control_plane.image_artifacts artifact ON artifact.id = version.role_image_artifact_id
WHERE agent.organization_id = $1::uuid AND agent.ref = $2
  AND artifact.organization_id = agent.organization_id
  AND artifact.scope_kind = environment.scope_kind
  AND artifact.project_id IS NOT DISTINCT FROM environment.project_id;
