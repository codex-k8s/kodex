-- name: platform__repository_bootstrap_update_installation_bootstrapped_at :exec
UPDATE control_plane.installation SET bootstrapped_at=clock_timestamp() WHERE singleton
