-- name: bootstrap_component_expire_schedule_claim :exec
UPDATE control_plane.schedule_occurrences
SET lease_expires_at = clock_timestamp() - interval '1 second'
WHERE ref = $1
