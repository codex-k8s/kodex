-- name: platform__repository_ready_1 :one
SELECT schema_version FROM control_plane.installation WHERE singleton
