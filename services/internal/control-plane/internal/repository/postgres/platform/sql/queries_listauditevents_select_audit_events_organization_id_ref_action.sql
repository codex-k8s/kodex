-- name: queries_listauditevents_select_audit_events_organization_id_ref_action :many
SELECT e.ref,
       COALESCE(scope_project.ref, ''),
       actor.ref,
       actor.display_name,
       COALESCE(assistant.name, ''),
       CASE WHEN assistant.id IS NULL THEN 'CONTROL_CENTER' ELSE 'SYSTEM_ASSISTANT' END,
       e.action,
       e.resource_kind,
       e.resource_ref,
       COALESCE(resource_project.name,
                resource_agent.name,
                resource_workflow.name,
                resource_run.title,
                resource_gate.title,
                resource_artifact.file_name,
                resource_schedule.name,
                resource_connection.name,
                resource_subject.display_name,
                e.safe_summary),
       e.outcome,
       e.safe_summary,
       e.correlation_ref,
       e.occurred_at
FROM control_plane.audit_events e
LEFT JOIN control_plane.projects scope_project ON scope_project.id = e.project_id
JOIN control_plane.subjects actor ON actor.id = e.actor_id
LEFT JOIN control_plane.agents assistant ON assistant.id = e.assistant_agent_id
LEFT JOIN control_plane.projects resource_project ON e.resource_kind = 'PROJECT' AND resource_project.organization_id = e.organization_id AND resource_project.ref = e.resource_ref
LEFT JOIN control_plane.agents resource_agent ON e.resource_kind IN ('AGENT', 'SYSTEM_ASSISTANT') AND resource_agent.organization_id = e.organization_id AND (resource_agent.ref = e.resource_ref OR resource_agent.system_key = e.resource_ref)
LEFT JOIN control_plane.workflows resource_workflow ON e.resource_kind = 'WORKFLOW' AND resource_workflow.organization_id = e.organization_id AND resource_workflow.ref = e.resource_ref
LEFT JOIN control_plane.runs resource_run ON e.resource_kind = 'RUN' AND resource_run.organization_id = e.organization_id AND resource_run.ref = e.resource_ref
LEFT JOIN control_plane.owner_gates resource_gate ON e.resource_kind = 'OWNER_GATE' AND resource_gate.organization_id = e.organization_id AND resource_gate.ref = e.resource_ref
LEFT JOIN control_plane.artifacts resource_artifact ON e.resource_kind = 'ARTIFACT' AND resource_artifact.organization_id = e.organization_id AND resource_artifact.ref = e.resource_ref
LEFT JOIN control_plane.schedules resource_schedule ON e.resource_kind = 'SCHEDULE' AND resource_schedule.organization_id = e.organization_id AND resource_schedule.ref = e.resource_ref
LEFT JOIN control_plane.integration_connections resource_connection ON e.resource_kind = 'INTEGRATION_CONNECTION' AND resource_connection.organization_id = e.organization_id AND resource_connection.ref = e.resource_ref
LEFT JOIN control_plane.memberships resource_membership ON e.resource_kind IN ('MEMBERSHIP', 'PLATFORM_MEMBERSHIP') AND resource_membership.organization_id = e.organization_id AND resource_membership.ref = e.resource_ref
LEFT JOIN control_plane.subjects resource_subject ON resource_subject.id = resource_membership.subject_id
WHERE e.organization_id = $1::uuid
  AND ($2 = '' OR scope_project.ref = $2)
  AND ($3 = '' OR e.action = $3)
  AND ($4 = '' OR e.outcome = $4)
  AND ($5 = '' OR COALESCE(resource_project.name,
                           resource_agent.name,
                           resource_workflow.name,
                           resource_run.title,
                           resource_gate.title,
                           resource_artifact.file_name,
                           resource_schedule.name,
                           resource_connection.name,
                           resource_subject.display_name,
                           e.safe_summary) ILIKE '%' || $5 || '%')
  AND ($6 IN ('OWNER', 'ADMINISTRATOR', 'AUDITOR')
       OR EXISTS(SELECT 1
                 FROM control_plane.memberships membership
                 WHERE membership.project_id = e.project_id
                   AND membership.subject_id = $7::uuid
                   AND membership.active
                   AND 'VIEW_AUDIT' = ANY(membership.permissions)))
  AND ($8::timestamptz IS NULL
       OR e.occurred_at < $8::timestamptz
       OR (e.occurred_at = $8::timestamptz AND e.ref > $9))
ORDER BY e.occurred_at DESC, e.ref
LIMIT $10
