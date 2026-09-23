-- name: project_purge__delete_database :one
SELECT control_plane.purge_project_database($1::uuid,$2::uuid,$3)
