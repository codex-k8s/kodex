-- name: platform__workers_mustscheduleref_select_schedules_id :one
SELECT ref FROM control_plane.schedules WHERE id=$1::uuid
