-- name: assistant_restore_update :one
UPDATE control_plane.assistant_conversations
SET state='ACTIVE', version=version+1, deleted_at=NULL, purge_after=NULL,
    updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 AND state='ARCHIVED'
RETURNING version,updated_at;
