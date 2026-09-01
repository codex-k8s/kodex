-- name: queries_listschedules_select_schedules_organization_id_ref_project_id :many
SELECT schedule.ref,
       project.ref,
       schedule.name,
       schedule.target_type,
       schedule.target_ref,
       COALESCE(agent.name, workflow.name, schedule.target_ref),
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
       schedule.updated_at,
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
       current_revision.created_at,
       schedule.continue_session_id IS NOT NULL,
       continue_session.ref,
       (@role IN ('OWNER', 'ADMINISTRATOR') OR EXISTS(
           SELECT 1
           FROM control_plane.memberships membership
           WHERE membership.project_id = schedule.project_id
             AND membership.subject_id = @actor_id::uuid
             AND membership.active
             AND 'MANAGE_SCHEDULES' = ANY(membership.permissions)
       ))
FROM control_plane.schedules schedule
JOIN control_plane.projects project
  ON project.id = schedule.project_id
 AND project.organization_id = schedule.organization_id
JOIN control_plane.schedule_revisions current_revision
  ON current_revision.id = schedule.current_revision_id
 AND current_revision.schedule_id = schedule.id
 AND current_revision.organization_id = schedule.organization_id
LEFT JOIN control_plane.agents agent
  ON schedule.target_type = 'AGENT'
 AND agent.ref = schedule.target_ref
 AND agent.organization_id = schedule.organization_id
 AND agent.project_id = schedule.project_id
LEFT JOIN control_plane.workflows workflow
  ON schedule.target_type = 'WORKFLOW'
 AND workflow.ref = schedule.target_ref
 AND workflow.organization_id = schedule.organization_id
 AND workflow.project_id = schedule.project_id
LEFT JOIN control_plane.sessions continue_session
  ON continue_session.id = schedule.continue_session_id
 AND continue_session.organization_id = schedule.organization_id
 AND continue_session.project_id = schedule.project_id
WHERE schedule.organization_id = @organization_id::uuid
  AND project.ref = @project_ref
  AND schedule.lifecycle_state <> 'DELETED'
  AND (@role IN ('OWNER', 'ADMINISTRATOR') OR EXISTS(
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.project_id = schedule.project_id
        AND membership.subject_id = @actor_id::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ))
  AND (@search_query = '' OR lower(concat_ws(
      ' ', schedule.name, schedule.target_ref, COALESCE(agent.name, ''), COALESCE(workflow.name, '')
  )) LIKE '%' || lower(@search_query) || '%')
  AND (@cursor_time::timestamptz IS NULL OR
       (schedule.updated_at, schedule.ref) < (@cursor_time::timestamptz, @cursor_ref))
ORDER BY schedule.updated_at DESC, schedule.ref DESC
LIMIT @page_size
