-- name: RuntimeAgentBindingGetForUpdate :one
SELECT organization_id, project_id, session_id, turn_id, attempt,
       input_sha256, runtime_revision_id, runtime_revision_version,
       runtime_revision_sha256, agent_session_key, agent_session_id,
       agent_session_version, agent_session_binding_sha256,
       agent_session_turn_id, agent_run_id, agent_session_turn_version,
       agent_turn_binding_sha256, created_at
FROM control_plane.runtime_agent_bindings
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND turn_id = @turn_id
  AND attempt = @attempt
FOR UPDATE;
