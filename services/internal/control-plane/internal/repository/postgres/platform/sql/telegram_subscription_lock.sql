-- name: telegram_subscription_lock :one
SELECT subscription.id::text,subscription.ref,subscription.version,connection.ref,subscription.enabled,
 subscription.event_types,subscription.scope_digest,subscription.configuration,subscription.expires_at,
 subscription.max_effects,subscription.used_effects,subscription.updated_at,COALESCE(subscription.grant_id::text,''),clock_timestamp(),
 EXISTS(SELECT 1 FROM control_plane.projects project JOIN control_plane.subjects actor ON actor.id=subscription.approved_by AND actor.active
  JOIN control_plane.integration_grants binding ON binding.id=subscription.grant_id AND binding.enabled
   AND binding.telegram_project_id=project.id AND binding.target_ref=subscription.ref AND binding.target_kind='TELEGRAM_SUBSCRIPTION'
   AND binding.resource_scope_digest=subscription.scope_digest
  WHERE project.id=subscription.project_id AND project.organization_id=subscription.organization_id AND project.lifecycle='ACTIVE'
   AND connection.enabled AND connection.lifecycle_state='ACTIVE' AND connection.credential_policy='PROJECT_SHARED'
   AND connection.state='CONNECTED' AND connection.version=subscription.connection_version
   AND connection.definition_version=subscription.definition_version AND connection.definition_digest=subscription.definition_digest
   AND connection.credential_revision_id=subscription.credential_revision_id AND connection.public_configuration=subscription.configuration
   AND control_plane.catalog_resource_visible(project.organization_id,actor.id,'project.manage','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
   AND control_plane.catalog_resource_visible(connection.organization_id,actor.id,'integration.manage','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp()))
FROM control_plane.telegram_subscriptions subscription
JOIN control_plane.integration_connections connection ON connection.id=subscription.connection_id
WHERE subscription.organization_id=$1::uuid AND subscription.project_id=$2::uuid
FOR UPDATE OF subscription;
