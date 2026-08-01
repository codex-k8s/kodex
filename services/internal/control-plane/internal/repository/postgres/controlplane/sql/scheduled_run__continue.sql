-- name: ScheduledRunContinue
UPDATE control_plane.scheduled_runs
SET state = 'CONTINUATION',
    continuation_turn_id = @continuation_turn_id::uuid,
    continuation_turn_version = @continuation_turn_version,
    continuation_runtime_revision_id = @continuation_runtime_revision_id::uuid,
    continuation_runtime_revision_version = @continuation_runtime_revision_version,
    continuation_input_sha256 = @continuation_input_sha256,
    owner_feedback_sha256 = @owner_feedback_sha256,
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
  AND state = 'WAITING_OWNER'
