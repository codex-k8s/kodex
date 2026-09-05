-- name: assistant_archive_update :one
WITH rejected AS (
    UPDATE control_plane.assistant_plans SET state='REJECTED',version=version+1
    WHERE organization_id=$1::uuid AND conversation_ref=$2 AND state IN ('DRAFT','VALID','INVALID','STALE')
)
UPDATE control_plane.assistant_conversations SET state='ARCHIVED',version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 AND state IN ('ACTIVE','CLOSED')
RETURNING version,updated_at;
