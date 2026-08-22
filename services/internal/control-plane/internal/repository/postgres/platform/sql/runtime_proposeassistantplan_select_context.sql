-- name: platform__runtime_proposeassistantplan_select_context :one
SELECT conversation.id::text,
       conversation.ref,
       conversation.version,
       COALESCE(conversation.project_id::text, ''),
       COALESCE(project.ref, ''),
       actor.id::text,
       actor.ref,
       actor.display_name,
       COALESCE(global_membership.role, 'MEMBER'),
       organization.ref
FROM control_plane.runs run
JOIN control_plane.assistant_conversations conversation
  ON conversation.organization_id = run.organization_id
 AND conversation.session_id = run.session_id
 AND conversation.state = 'ACTIVE'
JOIN control_plane.subjects actor
  ON actor.organization_id = run.organization_id
 AND actor.id = run.initiated_by
 AND actor.active
JOIN control_plane.organizations organization ON organization.id = run.organization_id
LEFT JOIN control_plane.projects project ON project.id = conversation.project_id
LEFT JOIN LATERAL (
    SELECT membership.role
    FROM control_plane.memberships membership
    WHERE membership.organization_id = run.organization_id
      AND membership.subject_id = actor.id
      AND membership.project_id IS NULL
      AND membership.active
    LIMIT 1
) global_membership ON true
WHERE run.organization_id = $1::uuid
  AND run.id = $2::uuid
  AND run.target_type = 'SYSTEM_ASSISTANT'
  AND EXISTS (
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.organization_id = run.organization_id
        AND membership.subject_id = actor.id
        AND membership.active
  )
FOR UPDATE OF conversation;
