-- name: runtime_proposeassistantplan_select_context :one
SELECT conversation.id::text,
       conversation.ref,
       conversation.version,
       COALESCE(conversation.project_id::text, ''),
       COALESCE(project.ref, ''),
       context.allowed_operations,
       run.target_ref,
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
JOIN LATERAL control_plane.assistant_context_projection(run.organization_id,actor.id,run.project_id,
    conversation.context_entity_kind,conversation.context_entity_ref,transaction_timestamp(),conversation.project_id) context ON true
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
  AND (conversation.project_id IS NULL OR (project.lifecycle='ACTIVE' AND control_plane.catalog_resource_visible(
      run.organization_id,actor.id,'project.view','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())))
  AND EXISTS (
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.organization_id = run.organization_id
        AND membership.subject_id = actor.id
        AND membership.active
  )
FOR UPDATE OF conversation;
