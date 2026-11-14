-- name: telegram_subscription_revoke :exec
WITH revoked AS (
 UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp()
 WHERE id=NULLIF(@grant_id,'')::uuid AND enabled RETURNING id
), cancelled AS (
 UPDATE control_plane.interaction_deliveries
 SET state=CASE WHEN telegram_effect_started_at IS NULL THEN 'CANCELLED' ELSE 'UNKNOWN_OUTCOME' END,
  safe_error_code=CASE WHEN telegram_effect_started_at IS NULL THEN 'TELEGRAM_SCOPE_REVOKED' ELSE 'TELEGRAM_OUTCOME_UNKNOWN' END,
  lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
  completed_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1
 WHERE telegram_subscription_id=@subscription_id::uuid AND state IN ('DUE','FAILED','CLAIMED')
 RETURNING id
)
UPDATE control_plane.telegram_subscriptions SET enabled=false,version=version+1,updated_at=clock_timestamp()
WHERE id=@subscription_id::uuid AND @disable::boolean;
