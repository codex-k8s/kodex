-- name: platform__artifacts_uploadartifact_2 :one
SELECT id::text,root_run_id::text,ref FROM control_plane.runs WHERE organization_id=$1::uuid AND project_id=$2::uuid AND ref=$3
