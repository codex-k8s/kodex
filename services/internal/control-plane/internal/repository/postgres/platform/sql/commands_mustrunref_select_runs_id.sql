-- name: platform__commands_mustrunref_select_runs_id :one
SELECT ref FROM control_plane.runs WHERE id=$1::uuid
