-- name: runtime_renewexecution_update_runtime_leases_expires_at_updated_at :exec
UPDATE control_plane.runtime_leases SET expires_at=$2,updated_at=clock_timestamp() WHERE id=$1::uuid
