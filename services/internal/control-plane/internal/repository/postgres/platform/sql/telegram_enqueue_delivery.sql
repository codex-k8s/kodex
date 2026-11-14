-- name: telegram_enqueue_delivery :exec
INSERT INTO control_plane.interaction_deliveries(ref,organization_id,project_id,connection_id,grant_id,root_run_id,
 capability_key,message_key,template_data,state,connection_version,definition_version,definition_digest,execution_max_attempts,
 telegram_subscription_id,telegram_subscription_version,telegram_scope_digest,telegram_credential_revision_id,
 telegram_input,telegram_input_digest,telegram_source_key,telegram_gate_id,telegram_expires_at,available_at)
SELECT @ref,subscription.organization_id,subscription.project_id,subscription.connection_id,subscription.grant_id,@root_run_id::uuid,
 'telegram.notifications',@template_key,'{}'::jsonb,'DUE',subscription.connection_version,subscription.definition_version,
 subscription.definition_digest,3,subscription.id,subscription.version,subscription.scope_digest,subscription.credential_revision_id,
 @snapshot::jsonb,@input_digest,@source_key,NULLIF(@gate_id,'')::uuid,@expires_at,@available_at
FROM control_plane.telegram_subscriptions subscription
WHERE subscription.id=@subscription_id::uuid AND subscription.organization_id=@organization_id::uuid
 AND subscription.enabled AND subscription.version=@subscription_version AND @event_type=ANY(subscription.event_types)
ON CONFLICT DO NOTHING;
