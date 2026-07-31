-- name: ScheduledRunSave
INSERT INTO control_plane.scheduled_runs (
    occurrence_id, attempt, session_id, session_version, turn_id, turn_version,
    process_run_id, process_version, runtime_revision_id,
    runtime_revision_version, effective_input_sha256, state, created_at
) VALUES (
    @occurrence_id::uuid, @attempt, @session_id::uuid, @session_version,
    @turn_id::uuid, @turn_version, nullif(@process_run_id, '')::uuid,
    nullif(@process_version, 0), @runtime_revision_id::uuid,
    @runtime_revision_version, @effective_input_sha256, @state, @created_at
)
