UPDATE control_plane.runtime_leases
SET expires_at = statement_timestamp() - interval '1 second'
WHERE ref = $1 AND state = 'CLAIMED';
