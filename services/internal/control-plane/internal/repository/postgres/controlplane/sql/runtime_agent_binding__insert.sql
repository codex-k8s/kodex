-- name: RuntimeAgentBindingInsert :exec
INSERT INTO control_plane.runtime_agent_bindings (
    organization_id, project_id, session_id, turn_id, attempt,
    input_sha256, runtime_revision_id, runtime_revision_version,
    runtime_revision_sha256, agent_session_key, agent_session_id,
    agent_session_version, agent_session_binding_sha256,
    agent_session_turn_id, agent_run_id, agent_session_turn_version,
    agent_turn_binding_sha256, created_at
) VALUES (
    @organization_id, @project_id, @session_id, @turn_id, @attempt,
    @input_sha256, @runtime_revision_id, @runtime_revision_version,
    @runtime_revision_sha256, @agent_session_key, @agent_session_id,
    @agent_session_version, @agent_session_binding_sha256,
    @agent_session_turn_id, @agent_run_id, @agent_session_turn_version,
    @agent_turn_binding_sha256, @created_at
);
