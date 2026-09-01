-- name: configuration_updateassistantconversationtitle_update_conversation :one
UPDATE control_plane.assistant_conversations
SET title=$2,title_source=$3,title_revision=title_revision+1,version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid
RETURNING ref,title,title_source,title_revision,state,version,created_at,updated_at
