-- name: telegram_expire_subscriptions :exec
WITH candidates AS MATERIALIZED (
 SELECT subscription.id FROM control_plane.telegram_subscriptions subscription
 WHERE subscription.organization_id=$1::uuid AND subscription.enabled
  AND (subscription.expires_at<=clock_timestamp() OR NOT control_plane.telegram_subscription_current(subscription.id))
 ORDER BY subscription.expires_at,subscription.id FOR UPDATE SKIP LOCKED LIMIT 100
), expired AS (
 UPDATE control_plane.telegram_subscriptions subscription
 SET enabled=false,version=version+1,updated_at=clock_timestamp()
 FROM candidates WHERE subscription.id=candidates.id RETURNING subscription.id,subscription.grant_id,subscription.expires_at
), revoked AS (
 UPDATE control_plane.integration_grants binding SET enabled=false,version=version+1,updated_at=clock_timestamp()
 FROM expired WHERE binding.id=expired.grant_id AND binding.enabled RETURNING binding.id
)
UPDATE control_plane.interaction_deliveries delivery
SET state=CASE WHEN telegram_effect_started_at IS NULL THEN 'CANCELLED' ELSE 'UNKNOWN_OUTCOME' END,
 safe_error_code=CASE WHEN telegram_effect_started_at IS NOT NULL THEN 'TELEGRAM_OUTCOME_UNKNOWN'
  WHEN expired.expires_at<=clock_timestamp() THEN 'TELEGRAM_SUBSCRIPTION_EXPIRED' ELSE 'TELEGRAM_SCOPE_REVOKED' END,
 lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 completed_at=clock_timestamp(),version=delivery.version+1,updated_at=clock_timestamp()
FROM expired WHERE delivery.telegram_subscription_id=expired.id AND delivery.state IN ('DUE','FAILED','CLAIMED');
