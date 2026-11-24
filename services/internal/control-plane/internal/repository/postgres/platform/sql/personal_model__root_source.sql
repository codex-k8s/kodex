-- name: personal_model__root_source :one
SELECT source FROM control_plane.runs
WHERE id = $1::uuid AND root_run_id = id AND organization_id = $2::uuid AND initiated_by = $3::uuid;
