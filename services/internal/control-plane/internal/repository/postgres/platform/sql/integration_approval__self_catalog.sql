-- name: integration_approval__self_catalog :many
SELECT 'ias_'||replace(approval.id::text,'-',''),approval.version,connection.ref,connection.name,
 approval.capability_key,agent.ref,root.ref,approval.scope_lifetime,
 CASE WHEN approval.revoked_at IS NOT NULL THEN 'REVOKED'
 WHEN approval.expires_at<=statement_timestamp() THEN 'EXPIRED'
 WHEN approval.reserved_effects>=approval.max_effects THEN 'EXHAUSTED' ELSE 'ACTIVE' END,
 approval.scope_paths,approval.scope_values::text,approval.max_effects,approval.reserved_effects,
 approval.created_at,approval.expires_at,approval.revoked_at
FROM control_plane.integration_approval_scopes approval
JOIN control_plane.integration_connections connection ON connection.id=approval.connection_id AND connection.organization_id=approval.organization_id
JOIN control_plane.runs root ON root.id=approval.root_run_id AND root.organization_id=approval.organization_id
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by AND initiator.organization_id=approval.organization_id AND initiator.kind='USER' AND initiator.active
JOIN control_plane.agents agent ON agent.id=approval.agent_id AND agent.organization_id=approval.organization_id
WHERE approval.organization_id=@organization_id::uuid AND root.initiated_by=@subject_id::uuid
 AND (approval.initiator_id IS NULL OR approval.initiator_id=root.initiated_by)
 AND ('ias_'||replace(approval.id::text,'-',''))>@after
 AND (@ref='' OR ('ias_'||replace(approval.id::text,'-',''))=@ref)
 AND control_plane.catalog_resource_visible(approval.organization_id,initiator.id,'integration.view','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp())
 AND control_plane.catalog_resource_visible(approval.organization_id,initiator.id,'run.view','RUN',root.id,root.project_id,root.initiated_by,'{}'::jsonb,statement_timestamp())
ORDER BY approval.id LIMIT @limit
