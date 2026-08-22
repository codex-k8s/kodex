-- name: runtime_completeexecution_update_assistant_conversations_version_updated_at :exec
UPDATE control_plane.assistant_conversations SET version=version+1,updated_at=clock_timestamp() WHERE session_id=$1::uuid
