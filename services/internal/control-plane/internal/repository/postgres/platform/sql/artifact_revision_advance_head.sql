-- name: artifact_revision_advance_head :exec
UPDATE control_plane.artifact_heads SET current_revision_id=$2::uuid,version=version+1
WHERE id=$1::uuid AND version=$3 AND lifecycle_state='ACTIVE'
