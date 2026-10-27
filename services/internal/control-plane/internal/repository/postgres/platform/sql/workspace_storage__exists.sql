-- name: workspace_storage__exists :one
SELECT to_regclass('control_plane.session_workspace_storage') IS NOT NULL;
