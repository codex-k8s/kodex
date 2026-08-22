-- name: platform__repository_bootstrap_12 :exec
UPDATE control_plane.installation SET bootstrapped_at=clock_timestamp() WHERE singleton
