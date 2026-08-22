-- name: platform__commands_resolvegate_update_change_request_session :exec
UPDATE control_plane.sessions
SET next_turn_number=next_turn_number+1,
    version=version+1,
    updated_at=clock_timestamp()
WHERE id=$1::uuid
