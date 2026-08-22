-- name: platform__repository_bootstrap_1 :one
SELECT bootstrapped_at FROM control_plane.installation WHERE singleton FOR UPDATE
