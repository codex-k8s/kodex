-- name: configuration_changeschedule_select_schedule_for_update :one
SELECT s.id::text,
       s.project_id::text,
       p.ref,
       s.preset,
       s.cron_expression,
       s.timezone,
       s.lifecycle_state,
       s.version,
       current_revision.ref,
       current_revision.revision,
       current_revision.digest,
       current_revision.name,
       current_revision.target_type,
       current_revision.target_ref,
       current_revision.preset,
       current_revision.cron_expression,
       current_revision.timezone,
       current_revision.input,
       current_revision.session_policy,
       current_revision.notification_policy,
       current_revision.created_at
FROM control_plane.schedules s
JOIN control_plane.projects p ON p.id = s.project_id
JOIN control_plane.schedule_revisions current_revision
  ON current_revision.id = s.current_revision_id
WHERE s.organization_id = @organization_id::uuid
  AND s.ref = @schedule_ref
  AND s.lifecycle_state <> 'DELETED'
FOR UPDATE
