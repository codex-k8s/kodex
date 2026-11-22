-- name: kubernetes_project__unreconciled :one
SELECT control_plane.kubernetes_project_unreconciled_count(@organization_id::uuid,@project_id::uuid);
