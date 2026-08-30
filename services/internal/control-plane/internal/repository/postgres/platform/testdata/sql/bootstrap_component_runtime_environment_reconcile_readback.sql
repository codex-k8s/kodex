-- name: bootstrap_component_runtime_environment_reconcile_readback :one
SELECT current_version.version_number,
       current_version.core_digest,
       current_version.digest,
       current_version.parent_version_id IS NOT NULL,
       (SELECT count(*)
        FROM control_plane.runtime_environment_versions version
        WHERE version.environment_set_id = environment.id),
       runtime.runtime_state,
       runtime.desired_runtime_revision,
       runtime.warm_instance_ref IS NULL,
       runtime.last_heartbeat_at IS NULL
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agent_runtime_environment_bindings binding
  ON binding.agent_id = runtime.agent_id
JOIN control_plane.runtime_environment_sets environment
  ON environment.id = binding.environment_set_id
JOIN control_plane.runtime_environment_versions current_version
  ON current_version.id = environment.current_version_id
WHERE runtime.stable_key = 'system-assistant';
