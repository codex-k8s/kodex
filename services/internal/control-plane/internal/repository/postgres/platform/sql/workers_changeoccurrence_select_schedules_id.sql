-- name: workers_changeoccurrence_select_schedules_id :one
SELECT s.ref,
       p.ref,
       s.name,
       s.target_type,
       s.target_ref,
       COALESCE(agent.name, workflow.name, s.target_ref),
       s.preset,
       s.cron_expression,
       s.timezone,
       s.input,
       s.session_policy,
       s.notification_policy,
       s.lifecycle_state,
       s.enabled,
       s.version,
       s.next_run_at,
       s.last_run_at,
       s.created_at,
       s.updated_at
FROM control_plane.schedules s
JOIN control_plane.projects p ON p.id = s.project_id
LEFT JOIN control_plane.agents agent ON s.target_type = 'AGENT' AND agent.ref = s.target_ref
LEFT JOIN control_plane.workflows workflow ON s.target_type = 'WORKFLOW' AND workflow.ref = s.target_ref
WHERE s.id = $1::uuid
