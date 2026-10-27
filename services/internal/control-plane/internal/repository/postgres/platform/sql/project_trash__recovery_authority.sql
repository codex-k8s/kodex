-- name: project_trash__recovery_authority :one
SELECT EXISTS (SELECT 1 FROM control_plane.project_purge_receipts
 WHERE organization_id=$1::uuid AND project_ref=$2 AND state IN ('PENDING','DONE'));
