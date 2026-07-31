-- name: ScheduleOccurrenceUpdate
UPDATE control_plane.schedule_occurrences
SET
    state = @state,
    attempt = @attempt,
    claimant_workload_id = nullif(@claimant_workload_id, ''),
    authority_generation = nullif(@authority_generation, 0),
    token_hash = nullif(@token_hash, ''),
    lease_expires_at = nullif(@lease_expires_at, 'epoch'::timestamptz),
    available_at = @available_at,
    outcome = nullif(@outcome, ''),
    result_artifact_id = nullif(@result_artifact_id, '')::uuid,
    updated_at = @updated_at
WHERE id = @id::uuid
  AND attempt = @expected_attempt
  AND (
      nullif(@expected_token_hash, '') IS NULL
      OR token_hash = @expected_token_hash
  )
