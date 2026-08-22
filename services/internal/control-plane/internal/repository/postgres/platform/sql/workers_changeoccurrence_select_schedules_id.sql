-- name: platform__workers_changeoccurrence_select_schedules_id :one
SELECT input FROM control_plane.schedules WHERE id=$1::uuid
