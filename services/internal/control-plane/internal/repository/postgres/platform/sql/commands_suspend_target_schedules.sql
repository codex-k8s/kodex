-- name: commands_suspend_target_schedules :one
WITH suspended AS (
    UPDATE control_plane.schedules schedule
    SET enabled = false,
        version = schedule.version + 1,
        updated_at = clock_timestamp()
    WHERE schedule.organization_id = $1::uuid
      AND schedule.project_id = $2::uuid
      AND schedule.target_type = $3
      AND schedule.target_ref = $4
      AND schedule.enabled
    RETURNING schedule.id, schedule.ref
), cancelled AS (
    UPDATE control_plane.schedule_occurrences occurrence
    SET state = 'CANCELLED',
        lease_ref = NULL,
        fence_digest = NULL,
        workload_instance = NULL,
        lease_expires_at = NULL,
        version = occurrence.version + 1,
        updated_at = clock_timestamp()
    WHERE occurrence.schedule_id IN (SELECT id FROM suspended)
      AND occurrence.state = 'CLAIMED'
    RETURNING occurrence.id
)
SELECT count(*), COALESCE(min(ref), '')
FROM suspended
