-- name: platform__repository_bootstrap_select_installation_singleton :one
SELECT bootstrapped_at FROM control_plane.installation WHERE singleton FOR UPDATE
