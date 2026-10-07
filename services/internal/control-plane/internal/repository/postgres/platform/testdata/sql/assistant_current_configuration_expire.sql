-- name: assistant_current_configuration_expire :one
WITH previous AS MATERIALIZED (
  SELECT ref, expires_at FROM control_plane.runtime_leases
  WHERE ref = $1 AND state = 'CLAIMED' FOR UPDATE
), changed AS (
  UPDATE control_plane.runtime_leases lease
  SET expires_at = clock_timestamp() - interval '1 second'
  FROM previous WHERE lease.ref = previous.ref RETURNING lease.ref
)
SELECT previous.expires_at FROM previous JOIN changed USING (ref);
