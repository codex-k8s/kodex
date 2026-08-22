-- name: platform__commands_launchrun_update_sessions_next_turn_number_version_updated_at :exec
UPDATE control_plane.sessions SET next_turn_number=next_turn_number+1,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
