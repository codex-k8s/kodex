-- name: ScheduledRunRebind
UPDATE control_plane.scheduled_runs
SET state = CASE WHEN state = 'WAITING_OWNER' THEN 'CONTINUATION' ELSE state END,
    current_session_id = @current_session_id::uuid,
    current_session_version = @current_session_version,
    current_turn_id = @current_turn_id::uuid,
    current_turn_version = @current_turn_version,
    current_turn_attempt = @current_turn_attempt,
    current_process_run_id = nullif(@current_process_run_id, '')::uuid,
    current_process_version = nullif(@current_process_version, 0),
    current_runtime_revision_id = @current_runtime_revision_id::uuid,
    current_runtime_revision_version = @current_runtime_revision_version,
    current_input_sha256 = @current_input_sha256,
    continuation_turn_id = nullif(@continuation_turn_id, '')::uuid,
    continuation_turn_version = nullif(@continuation_turn_version, 0),
    continuation_runtime_revision_id =
        nullif(@continuation_runtime_revision_id, '')::uuid,
    continuation_runtime_revision_version =
        nullif(@continuation_runtime_revision_version, 0),
    continuation_input_sha256 = nullif(@continuation_input_sha256, ''),
    owner_feedback_sha256 = nullif(@owner_feedback_sha256, '')
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
  AND current_turn_id = @expected_turn_id::uuid
  AND current_turn_attempt = @expected_turn_attempt
  AND state IN ('CLAIMED', 'WAITING_OWNER', 'CONTINUATION')
