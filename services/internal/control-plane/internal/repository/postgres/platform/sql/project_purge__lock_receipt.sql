-- name: project_purge__lock_receipt :one
SELECT state FROM control_plane.project_purge_receipts
WHERE organization_id=$1::uuid AND project_id=$2::uuid AND project_ref=$3
FOR UPDATE
