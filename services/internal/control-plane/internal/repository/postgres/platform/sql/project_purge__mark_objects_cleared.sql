-- name: project_purge__mark_objects_cleared :exec
UPDATE control_plane.project_purge_receipts
SET state='OBJECTS_CLEARED',objects_digest=$3,objects_cleared_at=statement_timestamp()
WHERE organization_id=$1::uuid AND project_id=$2::uuid AND state='PENDING'
