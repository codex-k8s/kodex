-- name: assistant_recipient_integration_catalog__context :one
SELECT recipient.kind,recipient.ref,context.entity_name,context.entity_version,
       project.ref,project.version
FROM control_plane.runtime_leases lease
JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.assistant_conversations conversation ON conversation.session_id=run.session_id AND conversation.organization_id=run.organization_id
JOIN control_plane.catalog_access_targets recipient ON recipient.organization_id=run.organization_id
 AND recipient.kind=run.assistant_context_entity_kind AND recipient.ref=run.assistant_context_entity_ref
JOIN control_plane.projects project ON project.id=recipient.project_id AND project.organization_id=run.organization_id AND project.id=run.project_id
JOIN LATERAL control_plane.assistant_context_projection_v2(run.organization_id,@actor_id::uuid,
 run.project_id,run.assistant_context_entity_kind,run.assistant_context_entity_ref,transaction_timestamp(),conversation.project_id) context ON true
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
 AND run.assistant_context_entity_kind IN ('AGENT','WORKFLOW') AND project.lifecycle='ACTIVE'
 AND 'CHANGE_INTEGRATION_GRANT'=ANY(context.allowed_operations)
 AND revision.safe_snapshot->'assistantContext'->>'entityKind'=recipient.kind
 AND revision.safe_snapshot->'assistantContext'->>'entityRef'=recipient.ref
 AND revision.safe_snapshot->'assistantContext'->>'entityVersion'=context.entity_version::text
 AND COALESCE(revision.safe_snapshot->'assistantContext'->'allowedOperations','[]'::jsonb) ? 'CHANGE_INTEGRATION_GRANT'
 AND (conversation.assistant_scope='SYSTEM' OR (conversation.assistant_scope='PROJECT' AND conversation.project_id=project.id))
 AND control_plane.catalog_resource_visible(run.organization_id,@actor_id::uuid,
 CASE recipient.kind WHEN 'AGENT' THEN 'agent.view' ELSE 'workflow.view' END,
 recipient.kind,recipient.id,recipient.project_id,recipient.owner_id,recipient.related_ids,transaction_timestamp());
