-- name: configuration_addassistantturncommand_update_assistant_conversations_latest_plan_id_version_updated_at :exec
UPDATE control_plane.assistant_conversations SET latest_plan_id=$2::uuid,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
