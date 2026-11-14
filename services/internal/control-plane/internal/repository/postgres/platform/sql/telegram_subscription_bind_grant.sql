-- name: telegram_subscription_bind_grant :exec
UPDATE control_plane.telegram_subscriptions subscription SET grant_id=grant_binding.id
FROM control_plane.integration_grants grant_binding
WHERE subscription.id=$1::uuid AND grant_binding.ref=$2
 AND grant_binding.organization_id=subscription.organization_id AND grant_binding.connection_id=subscription.connection_id
 AND grant_binding.target_kind='TELEGRAM_SUBSCRIPTION' AND grant_binding.target_ref=subscription.ref
 AND grant_binding.capability_key='telegram.notifications' AND grant_binding.resource_scope_digest=subscription.scope_digest;
