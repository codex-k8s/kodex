-- name: assistant_current_configuration_restore_expiry :exec
UPDATE control_plane.runtime_leases SET expires_at = $2
WHERE ref = $1 AND state = 'CLAIMED';
