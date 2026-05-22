-- name: session_state_snapshot__create :exec
INSERT INTO agent_manager_session_state_snapshots (
    id, session_id, run_id, snapshot_kind, turn_index, object_uri,
    object_digest, object_size_bytes, captured_at, created_at
) VALUES (
    @id, @session_id, @run_id::uuid, @snapshot_kind, @turn_index, @object_uri,
    @object_digest, @object_size_bytes, @captured_at, @created_at
);
