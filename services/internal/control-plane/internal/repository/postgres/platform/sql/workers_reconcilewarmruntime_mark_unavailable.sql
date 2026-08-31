-- name: workers_reconcilewarmruntime_mark_unavailable :one
UPDATE control_plane.assistant_runtime
SET runtime_state = 'UNAVAILABLE',
    warm_instance_ref = NULL,
    last_heartbeat_at = NULL,
    version = version + CASE
        WHEN runtime_state IS DISTINCT FROM 'UNAVAILABLE'
          OR warm_instance_ref IS NOT NULL
          OR last_heartbeat_at IS NOT NULL
        THEN 1
        ELSE 0
    END,
    updated_at = CASE
        WHEN runtime_state IS DISTINCT FROM 'UNAVAILABLE'
          OR warm_instance_ref IS NOT NULL
          OR last_heartbeat_at IS NOT NULL
        THEN clock_timestamp()
        ELSE updated_at
    END
WHERE organization_id = @organization_id::uuid
  AND system_session_ref = @session_ref
RETURNING version;
