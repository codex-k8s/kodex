-- name: platform__runtime_completeexecution_3 :exec
UPDATE control_plane.session_turns SET state=$2,completed_at=clock_timestamp() WHERE id=$1::uuid
