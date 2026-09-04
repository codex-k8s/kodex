-- name: workers_changeoccurrence_update_schedule_occurrences_state_run_id_version :exec
WITH materialized AS (
    UPDATE control_plane.schedule_occurrences
    SET state = 'MATERIALIZED', run_id = $2::uuid, lease_ref = NULL, fence_digest = NULL,
        workload_instance = NULL, lease_expires_at = NULL, version = version + 1,
        updated_at = clock_timestamp()
    WHERE id = $1::uuid AND state = 'CLAIMED'
    RETURNING schedule_id, schedule_revision_id, target_type, target_ref
)
UPDATE control_plane.schedules schedule
SET continue_session_id = CASE
        WHEN revision.session_policy = 'CONTINUE_ONE' AND schedule.session_policy = 'CONTINUE_ONE'
             AND schedule.target_type = materialized.target_type AND schedule.target_ref = materialized.target_ref
        THEN run.session_id ELSE schedule.continue_session_id END
FROM materialized
JOIN control_plane.schedule_revisions revision ON revision.id = materialized.schedule_revision_id
JOIN control_plane.runs run ON run.id = $2::uuid
WHERE schedule.id = materialized.schedule_id
