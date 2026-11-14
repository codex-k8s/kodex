SELECT NOT subscription.enabled AND NOT binding.enabled AND NOT EXISTS(
 SELECT 1 FROM control_plane.interaction_deliveries delivery
 WHERE delivery.telegram_subscription_id=subscription.id AND delivery.state IN ('DUE','CLAIMED','FAILED'))
FROM control_plane.telegram_subscriptions subscription
JOIN control_plane.integration_grants binding ON binding.id=subscription.grant_id
WHERE subscription.ref=$1;
