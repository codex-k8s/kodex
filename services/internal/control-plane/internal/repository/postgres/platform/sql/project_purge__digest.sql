-- name: project_purge__digest :one
SELECT control_plane.project_purge_inventory_digest($1::uuid,$2::uuid)
