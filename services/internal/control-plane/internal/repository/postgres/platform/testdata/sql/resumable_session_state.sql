-- name: resumable_session_state :exec
UPDATE control_plane.sessions SET state = $2 WHERE ref = $1;
