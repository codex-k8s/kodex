-- name: target_snapshot__lock :one
SELECT control_plane.lock_legacy_cutover_resources();
