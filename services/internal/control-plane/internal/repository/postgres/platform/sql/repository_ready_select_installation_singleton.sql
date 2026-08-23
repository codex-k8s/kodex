-- name: repository_ready_select_installation_singleton :one
SELECT schema_version FROM control_plane.installation WHERE singleton
