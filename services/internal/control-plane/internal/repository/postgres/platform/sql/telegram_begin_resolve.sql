-- name: telegram_begin_resolve :one
SELECT delivery.id::text,subscription.id::text,delivery.ref,delivery.telegram_input,delivery.telegram_input_digest,
 delivery.telegram_effect_started_at,delivery.telegram_expires_at,clock_timestamp(),subscription.rate_window_started_at,
 subscription.rate_window_effects,subscription.configuration,
 subscription.version=delivery.telegram_subscription_version AND subscription.scope_digest=delivery.telegram_scope_digest
  AND subscription.credential_revision_id=delivery.telegram_credential_revision_id
  AND control_plane.telegram_subscription_eligible(subscription.id,delivery.root_run_id,delivery.telegram_gate_id,delivery.telegram_input->>'event_type')
FROM control_plane.interaction_deliveries delivery
JOIN control_plane.telegram_subscriptions subscription ON subscription.id=delivery.telegram_subscription_id
JOIN control_plane.integration_connections connection ON connection.id=delivery.connection_id AND connection.organization_id=delivery.organization_id
JOIN control_plane.integration_grants binding ON binding.id=delivery.grant_id AND binding.organization_id=delivery.organization_id
WHERE delivery.organization_id=@organization_id::uuid AND delivery.ref=@delivery_ref
 AND delivery.capability_key='telegram.notifications' AND delivery.state='CLAIMED'
 AND delivery.lease_ref=@lease_ref AND delivery.fence_digest=encode(digest(@fence,'sha256'),'hex')
 AND delivery.generation=@generation AND delivery.lease_expires_at>clock_timestamp()
FOR UPDATE OF subscription,connection,binding,delivery;
