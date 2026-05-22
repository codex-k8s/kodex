-- name: session_state_snapshot__get :one
SELECT
    id,
    session_id,
    run_id,
    snapshot_kind,
    turn_index,
    object_uri,
    object_digest,
    object_size_bytes,
    captured_at,
    created_at
FROM agent_manager_session_state_snapshots
WHERE id = @id;
