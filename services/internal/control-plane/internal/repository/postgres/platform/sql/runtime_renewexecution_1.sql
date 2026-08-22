-- name: platform__runtime_renewexecution_1 :exec
UPDATE control_plane.runtime_leases SET expires_at=$2,updated_at=clock_timestamp() WHERE id=$1::uuid
