-- name: platform__workers_mustscheduleref_1 :one
SELECT ref FROM control_plane.schedules WHERE id=$1::uuid
