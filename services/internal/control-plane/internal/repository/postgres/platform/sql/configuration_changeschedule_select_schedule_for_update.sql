-- name: configuration_changeschedule_select_schedule_for_update :one
SELECT s.id::text,
       s.project_id::text,
       p.ref,
       s.preset,
       s.cron_expression,
       s.timezone,
       s.lifecycle_state,
       s.version
FROM control_plane.schedules s
JOIN control_plane.projects p ON p.id = s.project_id
WHERE s.organization_id = @organization_id::uuid
  AND s.ref = @schedule_ref
  AND s.lifecycle_state <> 'DELETED'
FOR UPDATE
