-- name: platform__artifacts_uploadartifact_select_runs_organization_id_project_id_ref :one
SELECT id::text,root_run_id::text,ref FROM control_plane.runs WHERE organization_id=$1::uuid AND project_id=$2::uuid AND ref=$3
