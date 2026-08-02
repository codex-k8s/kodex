-- name: ScheduleOccurrenceUpdate
UPDATE control_plane.schedule_occurrences
SET
    state = @state,
    attempt = @attempt,
    claimant_workload_id = nullif(@claimant_workload_id, ''),
    authority_generation = nullif(@authority_generation, 0),
    token_hash = nullif(@token_hash, ''),
    claim_key_sha256 = nullif(@claim_key_sha256, ''),
    lease_expires_at = nullif(@lease_expires_at, 'epoch'::timestamptz),
    available_at = @available_at,
    outcome = nullif(@outcome, ''),
    result_artifact_id = nullif(@result_artifact_id, '')::uuid,
    execution_session_id = nullif(@execution_session_id, '')::uuid,
    execution_session_version = nullif(@execution_session_version, 0),
    execution_turn_id = nullif(@execution_turn_id, '')::uuid,
    execution_turn_version = nullif(@execution_turn_version, 0),
    execution_process_run_id = nullif(@execution_process_run_id, '')::uuid,
    execution_process_version = nullif(@execution_process_version, 0),
    execution_runtime_revision_id = nullif(@execution_runtime_revision_id, '')::uuid,
    execution_runtime_revision_version = nullif(@execution_runtime_revision_version, 0),
    updated_at = @updated_at
WHERE id = @id::uuid
  AND attempt = @expected_attempt
  AND (
      nullif(@expected_token_hash, '') IS NULL
      OR token_hash = @expected_token_hash
  )
