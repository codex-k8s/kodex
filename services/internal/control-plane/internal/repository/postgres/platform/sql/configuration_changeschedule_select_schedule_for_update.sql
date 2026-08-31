-- name: configuration_changeschedule_select_schedule_for_update :one
SELECT s.id::text,
       s.project_id::text,
       p.ref,
       s.preset,
       s.cron_expression,
       s.timezone,
       s.lifecycle_state,
       s.version,
       current_revision.revision
FROM control_plane.schedules s
JOIN control_plane.projects p ON p.id = s.project_id
JOIN control_plane.schedule_revisions current_revision
  ON current_revision.id = s.current_revision_id
WHERE s.organization_id = @organization_id::uuid
  AND s.ref = @schedule_ref
  AND s.lifecycle_state <> 'DELETED'
FOR UPDATE
