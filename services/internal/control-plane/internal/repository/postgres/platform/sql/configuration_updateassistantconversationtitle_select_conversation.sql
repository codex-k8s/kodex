-- name: configuration_updateassistantconversationtitle_select_conversation :one
SELECT id::text,COALESCE(project_id::text,''),version,title_revision,state
FROM control_plane.assistant_conversations
WHERE organization_id=$1::uuid AND ref=$2
FOR UPDATE
