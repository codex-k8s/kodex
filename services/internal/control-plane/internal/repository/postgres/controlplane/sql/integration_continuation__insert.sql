INSERT INTO control_plane.integration_continuations (
    id, organization_id, project_id, process_id, session_id, session_version,
    thread_id, role_id, turn_id, turn_version, attempt, runtime_revision_id,
    runtime_revision_version, runtime_revision_sha256, immutable_input_sha256,
    grant_generation, invocation_id, approval_id, integration_id,
    request_sha256, approval_state, execution_state, continuation_state,
    version, fence, approval_expires_at, decision_reference, decision_sha256,
    result_reference, result_sha256, error_code, error_reference, error_sha256,
    continuation_turn_id, continuation_turn_version,
    continuation_runtime_revision_id, continuation_runtime_revision_version,
    continuation_input_sha256, created_at, updated_at
) VALUES (
    @id, @organization_id, @project_id, @process_id, @session_id, @session_version,
    @thread_id, @role_id, @turn_id, @turn_version, @attempt, @runtime_revision_id,
    @runtime_revision_version, @runtime_revision_sha256, @immutable_input_sha256,
    @grant_generation, @invocation_id, @approval_id, @integration_id,
    @request_sha256, @approval_state, @execution_state, @continuation_state,
    @version, @fence, @approval_expires_at, nullif(@decision_reference, ''),
    nullif(@decision_sha256, ''), nullif(@result_reference, ''),
    nullif(@result_sha256, ''), nullif(@error_code, ''),
    nullif(@error_reference, ''), nullif(@error_sha256, ''),
    nullif(@continuation_turn_id, '')::uuid,
    nullif(@continuation_turn_version, 0),
    nullif(@continuation_runtime_revision_id, '')::uuid,
    nullif(@continuation_runtime_revision_version, 0),
    nullif(@continuation_input_sha256, ''), @created_at, @updated_at
);
