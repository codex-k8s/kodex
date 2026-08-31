-- name: workers_reconcilewarmruntime_switch_session :one
UPDATE control_plane.assistant_runtime
SET system_session_ref = @next_session_ref,
    runtime_state = 'RECOVERING',
    warm_instance_ref = NULL,
    last_heartbeat_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND system_session_ref = @current_session_ref
RETURNING version;
