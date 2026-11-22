-- name: kubernetes_manual_project_snapshot :one
SELECT version,lifecycle FROM control_plane.projects
WHERE organization_id=$1::uuid AND id=$2::uuid;
