-- name: repository_bootstrap_select_assistant_runtime_environment :one
SELECT runtime.organization_id::text,
       runtime.agent_id::text,
       environment.id::text,
       current_version.id::text,
       current_version.version_number,
       current_version.non_secret_values,
       current_version.secret_descriptors,
       current_version.selected_tools,
       current_version.core_digest,
       current_version.digest,
       current_version.resource_policy,
       current_version.volume_policy,
       current_version.network_policy,
       current_version.kubernetes_access_profile,
       current_version.resources_digest,
       current_version.volumes_digest,
       current_version.network_digest,
       current_version.rbac_digest
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent
  ON agent.id = runtime.agent_id
 AND agent.system_key = 'system-assistant'
 AND agent.project_id IS NULL
JOIN control_plane.agent_runtime_environment_bindings binding
  ON binding.agent_id = agent.id
JOIN control_plane.runtime_environment_sets environment
  ON environment.id = binding.environment_set_id
 AND environment.organization_id = runtime.organization_id
 AND environment.project_id IS NULL
 AND environment.state = 'ACTIVE'
JOIN control_plane.runtime_environment_versions current_version
  ON current_version.id = environment.current_version_id
 AND current_version.organization_id = runtime.organization_id
 AND current_version.role_image_artifact_id IS NULL
WHERE runtime.stable_key = 'system-assistant'
FOR UPDATE OF runtime, environment;
