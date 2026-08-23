-- name: runtime_callback_update_session :exec
UPDATE control_plane.sessions
SET next_turn_number = next_turn_number + 1,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @session_id::uuid
