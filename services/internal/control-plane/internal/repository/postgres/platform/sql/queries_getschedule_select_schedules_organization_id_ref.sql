-- name: queries_getschedule_select_schedules_organization_id_ref :one
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
       ($3 IN ('OWNER', 'ADMINISTRATOR') OR EXISTS(
           SELECT 1
           FROM control_plane.memberships membership
           WHERE membership.project_id = schedule.project_id
             AND membership.subject_id = $4::uuid
             AND membership.active
             AND 'MANAGE_SCHEDULES' = ANY(membership.permissions)
       ))
FROM control_plane.schedules schedule
JOIN control_plane.projects project ON project.id = schedule.project_id
LEFT JOIN control_plane.agents agent
  ON schedule.target_type = 'AGENT' AND agent.ref = schedule.target_ref
LEFT JOIN control_plane.workflows workflow
  ON schedule.target_type = 'WORKFLOW' AND workflow.ref = schedule.target_ref
WHERE schedule.organization_id = $1::uuid
  AND schedule.ref = $2
  AND schedule.lifecycle_state <> 'DELETED'
  AND ($3 IN ('OWNER', 'ADMINISTRATOR') OR EXISTS(
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.project_id = schedule.project_id
        AND membership.subject_id = $4::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ))
