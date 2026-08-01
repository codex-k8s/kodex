-- name: ScheduledRunGetForUpdate
SELECT
    occurrence_id::text,
    attempt,
    session_id::text,
    session_version,
    turn_id::text,
    turn_version,
    coalesce(process_run_id::text, ''),
    coalesce(process_version, 0),
    runtime_revision_id::text,
    runtime_revision_version,
    effective_input_sha256,
    state,
    coalesce(outcome, ''),
    coalesce(result_artifact_id::text, ''),
    created_at,
    coalesce(finished_at, 'epoch'::timestamptz),
    coalesce(continuation_turn_id::text, ''),
    coalesce(continuation_turn_version, 0),
    coalesce(continuation_runtime_revision_id::text, ''),
    coalesce(continuation_runtime_revision_version, 0),
    coalesce(continuation_input_sha256, ''),
    coalesce(owner_feedback_sha256, ''),
    current_session_id::text,
    current_session_version,
    current_turn_id::text,
    current_turn_version,
    current_turn_attempt,
    coalesce(current_process_run_id::text, ''),
    coalesce(current_process_version, 0),
    current_runtime_revision_id::text,
    current_runtime_revision_version,
    current_input_sha256
FROM control_plane.scheduled_runs
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
FOR UPDATE
