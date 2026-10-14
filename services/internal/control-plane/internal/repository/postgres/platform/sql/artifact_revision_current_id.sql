-- name: artifact_revision_current_id :one
SELECT revision.id::text FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id
WHERE head.organization_id=$1::uuid AND head.ref=$2 AND revision.ref=$3 AND head.lifecycle_state='ACTIVE'
