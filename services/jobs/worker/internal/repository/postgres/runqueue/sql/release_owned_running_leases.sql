-- name: runqueue__release_owned_running_leases :many
WITH candidates AS (
    SELECT r.id,
           r.correlation_id,
           r.project_id,
           COALESCE(r.lease_owner, '') AS previous_lease_owner,
           r.lease_until AS previous_lease_until
    FROM agent_runs AS r
    WHERE r.status = 'running'
      AND r.lease_owner = $1
    ORDER BY r.started_at NULLS FIRST, r.created_at ASC
    FOR UPDATE
),
released AS (
    UPDATE agent_runs AS r
    SET lease_owner = NULL,
        lease_until = NULL,
        stale_reclaim_pending = TRUE,
        updated_at = NOW()
    FROM candidates AS c
    WHERE r.id = c.id
    RETURNING r.id::text AS run_id,
              c.correlation_id,
              c.project_id,
              c.previous_lease_owner,
              c.previous_lease_until,
              'stopped'::text AS worker_status
)
SELECT run_id,
       correlation_id,
       project_id,
       previous_lease_owner,
       previous_lease_until,
       worker_status
FROM released
ORDER BY run_id;
