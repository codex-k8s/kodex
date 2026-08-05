UPDATE control_plane.schedule_occurrence_capabilities
SET state = @state,
    consumed_at = nullif(@consumed_at, 'epoch'::timestamptz),
    revoked_at = nullif(@revoked_at, 'epoch'::timestamptz)
WHERE id = @id::uuid
  AND state = @expected_state
