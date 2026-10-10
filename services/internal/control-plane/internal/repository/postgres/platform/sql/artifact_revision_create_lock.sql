-- name: artifact_revision_create_lock :one
SELECT head.id::text, head.project_id::text, head.version
FROM control_plane.artifact_heads head
WHERE head.organization_id=$1::uuid AND head.ref=$2 AND head.lifecycle_state='ACTIVE'
FOR UPDATE OF head
