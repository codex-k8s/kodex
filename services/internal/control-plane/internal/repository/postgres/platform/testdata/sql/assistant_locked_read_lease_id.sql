-- name: assistant_locked_read_lease_id :one
SELECT id::text FROM control_plane.runtime_leases WHERE ref = $1;
