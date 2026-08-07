-- name: target_runtime_executions__list :many
SELECT jsonb_build_object(
    'id', execution.id,
    'organization_id', execution.organization_id,
    'project_id', execution.project_id,
    'owner_actor_id', process.owner_actor_id,
    'kind', 'RUNTIME_EXECUTION',
    'state', execution.state,
    'version', execution.version,
    'spec', jsonb_build_object(
        'processRunId', execution.process_id,
        'sessionId', execution.session_id,
        'turnId', execution.turn_id,
        'attempt', execution.attempt,
        'runtimeRevisionId', execution.runtime_revision_id,
        'runtimeRevisionVersion', execution.runtime_revision_version,
        'runtimeRevisionSha256', execution.runtime_revision_sha256,
        'immutableInputSha256', execution.immutable_input_sha256
    )
)::text
FROM control_plane.runtime_executions AS execution
JOIN control_plane.resources AS process ON process.id = execution.process_id AND process.kind = 'PROCESS_RUN'
ORDER BY execution.organization_id, execution.project_id, execution.id;
