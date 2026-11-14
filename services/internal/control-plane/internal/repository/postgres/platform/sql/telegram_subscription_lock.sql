-- name: telegram_subscription_lock :one
SELECT subscription.id::text,subscription.ref,subscription.version,connection.ref,subscription.enabled,
 subscription.event_types,subscription.scope_digest,subscription.configuration,subscription.expires_at,
 subscription.max_effects,subscription.used_effects,subscription.updated_at,COALESCE(subscription.grant_id::text,'')
FROM control_plane.telegram_subscriptions subscription
JOIN control_plane.integration_connections connection ON connection.id=subscription.connection_id
WHERE subscription.organization_id=$1::uuid AND subscription.project_id=$2::uuid
FOR UPDATE OF subscription;
