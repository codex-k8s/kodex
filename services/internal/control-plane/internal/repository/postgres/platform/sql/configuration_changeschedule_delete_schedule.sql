-- name: configuration_changeschedule_delete_schedule :one
UPDATE control_plane.schedules schedule
SET lifecycle_state = 'DELETED',
    enabled = false,
    next_run_at = NULL,
    version = schedule.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.projects project
WHERE schedule.project_id = project.id
  AND schedule.organization_id = @organization_id::uuid
  AND schedule.ref = @schedule_ref
  AND schedule.version = @expected_version
  AND schedule.lifecycle_state = 'ARCHIVED'
RETURNING schedule.project_id::text,
          project.ref,
          schedule.ref,
          schedule.name,
          schedule.target_type,
          schedule.target_ref,
          schedule.preset,
          schedule.cron_expression,
          schedule.timezone,
          schedule.input,
          schedule.session_policy,
          schedule.notification_policy,
          schedule.lifecycle_state,
          schedule.enabled,
          schedule.version,
          schedule.next_run_at,
          schedule.last_run_at,
          schedule.created_at,
          schedule.updated_at
