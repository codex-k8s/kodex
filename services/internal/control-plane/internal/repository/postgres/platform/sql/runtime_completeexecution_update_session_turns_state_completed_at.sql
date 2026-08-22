-- name: platform__runtime_completeexecution_update_session_turns_state_completed_at :exec
UPDATE control_plane.session_turns SET state=$2,completed_at=clock_timestamp() WHERE id=$1::uuid
