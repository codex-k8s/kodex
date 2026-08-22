-- name: platform__workers_claimdueschedules_1 :many
SELECT s.id::text,s.ref,s.next_run_at,s.version FROM control_plane.schedules s WHERE s.organization_id=$1::uuid AND s.enabled AND s.next_run_at<=clock_timestamp() ORDER BY s.next_run_at FOR UPDATE SKIP LOCKED LIMIT $2
