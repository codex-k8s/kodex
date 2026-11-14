-- name: telegram_source_candidate :one
SELECT subscription.id::text,subscription.ref,subscription.version,subscription.scope_digest,subscription.configuration,
 subscription.expires_at,subscription.connection_version,subscription.definition_digest,credential.ref,
 connection.id::text,subscription.grant_id::text,run.ref,run.version,run.state,project.ref,project.language,
 COALESCE(gate.ref,''),COALESCE(gate.version,0),clock_timestamp(),
 (SELECT count(*) FROM control_plane.interaction_deliveries pending
  WHERE pending.telegram_subscription_id=subscription.id AND pending.state IN ('DUE','FAILED','CLAIMED'))
FROM control_plane.telegram_subscriptions subscription
JOIN control_plane.projects project ON project.id=subscription.project_id AND project.organization_id=subscription.organization_id
JOIN control_plane.subjects actor ON actor.id=subscription.approved_by AND actor.active
JOIN control_plane.integration_connections connection ON connection.id=subscription.connection_id
 AND connection.organization_id=subscription.organization_id AND connection.definition_key='telegram'
 AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='CONNECTED'
 AND connection.credential_policy='PROJECT_SHARED' AND control_plane.integration_connection_configuration_ready(connection.id)
 AND connection.version=subscription.connection_version AND connection.definition_version=subscription.definition_version
 AND connection.definition_digest=subscription.definition_digest AND connection.credential_revision_id=subscription.credential_revision_id
 AND connection.public_configuration=subscription.configuration
JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
 AND credential.organization_id=subscription.organization_id AND credential.connection_id=connection.id
JOIN control_plane.integration_grants grant_binding ON grant_binding.id=subscription.grant_id
 AND grant_binding.telegram_project_id=subscription.project_id
 AND grant_binding.organization_id=subscription.organization_id AND grant_binding.connection_id=connection.id
 AND grant_binding.target_kind='TELEGRAM_SUBSCRIPTION' AND grant_binding.target_ref=subscription.ref
 AND grant_binding.enabled AND grant_binding.capability_key='telegram.notifications' AND grant_binding.approval_policy='HUMAN_SCOPED'
 AND grant_binding.resource_scope_digest=subscription.scope_digest
 AND grant_binding.definition_version=subscription.definition_version AND grant_binding.definition_digest=subscription.definition_digest
JOIN control_plane.runs run ON run.id=@root_run_id::uuid AND run.organization_id=subscription.organization_id
 AND run.project_id=project.id AND run.id=run.root_run_id
LEFT JOIN control_plane.owner_gates gate ON gate.id=NULLIF(@gate_id,'')::uuid AND gate.organization_id=subscription.organization_id
 AND gate.root_run_id=run.id AND gate.project_id=project.id
WHERE subscription.organization_id=@organization_id::uuid AND subscription.project_id=@project_id::uuid
 AND subscription.enabled AND subscription.expires_at>clock_timestamp() AND subscription.used_effects<subscription.max_effects
 AND project.lifecycle='ACTIVE'
 AND ((@gate_id='' AND run.state IN ('SUCCEEDED','FAILED')) OR (@gate_id<>'' AND gate.state='OPEN' AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELLING')))
 AND control_plane.catalog_resource_visible(project.organization_id,actor.id,'project.manage','PROJECT',
  project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
 AND control_plane.catalog_resource_visible(connection.organization_id,actor.id,'integration.manage','INTEGRATION',
  connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
 AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
  WHERE target.organization_id=run.organization_id AND target.kind='RUN' AND target.id=run.id
   AND control_plane.catalog_resource_visible(run.organization_id,actor.id,'run.view',target.kind,
    target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp()))
FOR UPDATE OF subscription;
