-- name: queries_listschedules_select_schedules_organization_id_ref_project_id :many
SELECT s.ref,p.ref,s.name,s.target_type,s.target_ref,COALESCE(a.name,w.name,s.target_ref),s.preset,s.cron_expression,s.timezone,s.input,s.session_policy,s.notification_policy,s.lifecycle_state,s.enabled,s.version,s.next_run_at,s.last_run_at,s.created_at,s.updated_at,
       ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=s.project_id AND m.subject_id=$4::uuid AND m.active AND 'MANAGE_SCHEDULES'=ANY(m.permissions)))
FROM control_plane.schedules s
JOIN control_plane.projects p ON p.id=s.project_id
LEFT JOIN control_plane.agents a ON s.target_type='AGENT' AND a.ref=s.target_ref
LEFT JOIN control_plane.workflows w ON s.target_type='WORKFLOW' AND w.ref=s.target_ref
WHERE s.organization_id=$1::uuid
  AND p.ref=$2
  AND s.lifecycle_state <> 'DELETED'
  AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=s.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
  AND ($5 = '' OR lower(concat_ws(' ', s.name, s.target_ref, COALESCE(a.name, ''), COALESCE(w.name, ''))) LIKE '%' || lower($5) || '%')
  AND ($6::timestamptz IS NULL OR (s.updated_at, s.ref) < ($6::timestamptz, $7))
ORDER BY s.updated_at DESC, s.ref DESC
LIMIT $8
