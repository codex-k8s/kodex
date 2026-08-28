-- name: workers_claimdueschedules_select_schedules_organization_id :many
SELECT s.id::text,
       s.ref,
       s.next_run_at,
       s.version,
       s.preset,
       s.cron_expression,
       s.timezone,
       s.name,
       s.target_type,
       s.target_ref,
       s.input
FROM control_plane.schedules s
WHERE s.organization_id = $1::uuid
  AND s.lifecycle_state = 'ACTIVE'
  AND s.enabled
  AND s.next_run_at <= clock_timestamp()
  AND NOT EXISTS(SELECT 1
                 FROM control_plane.schedule_occurrences occurrence
                 WHERE occurrence.schedule_id = s.id
                   AND occurrence.state IN ('CLAIMED', 'MATERIALIZED'))
ORDER BY s.next_run_at
FOR UPDATE SKIP LOCKED
LIMIT $2
