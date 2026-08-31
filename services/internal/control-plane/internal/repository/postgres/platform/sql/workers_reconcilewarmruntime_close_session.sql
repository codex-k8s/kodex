-- name: workers_reconcilewarmruntime_close_session :exec
UPDATE control_plane.sessions
SET state = 'CLOSED',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND id = @session_id::uuid
  AND state = 'ACTIVE';
