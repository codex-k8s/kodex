-- name: ScheduledRunSuspendExternal
UPDATE control_plane.scheduled_runs
SET state = 'CONTINUATION',
    outcome = NULL,
    result_artifact_id = NULL,
    finished_at = NULL,
    current_session_id = @current_session_id::uuid,
    current_session_version = @current_session_version,
    current_turn_id = @current_turn_id::uuid,
    current_turn_version = @current_turn_version,
    current_turn_attempt = @current_turn_attempt,
    current_process_run_id = @current_process_run_id::uuid,
    current_process_version = @current_process_version,
    current_runtime_revision_id = @current_runtime_revision_id::uuid,
    current_runtime_revision_version = @current_runtime_revision_version,
    current_input_sha256 = @current_input_sha256
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
  AND current_turn_id = @expected_turn_id::uuid
  AND current_turn_attempt = @expected_turn_attempt
  AND state IN ('CLAIMED', 'CONTINUATION')
