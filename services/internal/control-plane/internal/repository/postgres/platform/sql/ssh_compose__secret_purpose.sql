-- name: ssh_compose__secret_purpose :one
SELECT purpose FROM control_plane.runtime_secrets WHERE id=@secret_id::uuid FOR SHARE;
