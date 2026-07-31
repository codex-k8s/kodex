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
    coalesce(finished_at, 'epoch'::timestamptz)
FROM control_plane.scheduled_runs
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
FOR UPDATE
