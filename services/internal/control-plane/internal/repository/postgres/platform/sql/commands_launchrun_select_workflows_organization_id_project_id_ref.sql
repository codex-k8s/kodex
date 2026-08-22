-- name: platform__commands_launchrun_select_workflows_organization_id_project_id_ref :one
SELECT name,published_spec FROM control_plane.workflows WHERE organization_id=$1::uuid AND project_id=$2::uuid AND ref=$3 AND state='PUBLISHED'
