-- name: platform__runtime_completeexecution_15 :exec
UPDATE control_plane.assistant_conversations SET version=version+1,updated_at=clock_timestamp() WHERE session_id=$1::uuid
