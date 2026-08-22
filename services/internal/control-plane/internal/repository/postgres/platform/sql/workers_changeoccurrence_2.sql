-- name: platform__workers_changeoccurrence_2 :one
SELECT input FROM control_plane.schedules WHERE id=$1::uuid
