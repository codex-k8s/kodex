-- name: telegram_complete_resolve :one
SELECT delivery.id::text,delivery.project_id::text,project.ref,delivery.telegram_input,delivery.telegram_input_digest,
 delivery.telegram_effect_started_at,delivery.telegram_subscription_id::text,delivery.telegram_subscription_version,
 delivery.attempt,delivery.execution_max_attempts,delivery.telegram_expires_at,clock_timestamp()
FROM control_plane.interaction_deliveries delivery
JOIN control_plane.projects project ON project.id=delivery.project_id AND project.organization_id=delivery.organization_id
JOIN control_plane.telegram_subscriptions subscription ON subscription.id=delivery.telegram_subscription_id
WHERE delivery.organization_id=@organization_id::uuid AND delivery.ref=@delivery_ref AND delivery.capability_key='telegram.notifications'
 AND delivery.state='CLAIMED' AND delivery.lease_ref=@lease_ref AND delivery.fence_digest=encode(digest(@fence,'sha256'),'hex')
 AND delivery.generation=@generation AND delivery.lease_expires_at>clock_timestamp()
FOR UPDATE OF subscription,delivery;
