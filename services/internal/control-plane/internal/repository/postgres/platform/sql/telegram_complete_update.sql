-- name: telegram_complete_update :exec
WITH refunded AS (
 UPDATE control_plane.telegram_subscriptions SET used_effects=used_effects-1
 WHERE id=@subscription_id::uuid AND version=@subscription_version AND used_effects>0 AND @refund::boolean
 RETURNING id
)
UPDATE control_plane.interaction_deliveries
SET state=@state,safe_error_code=@safe_error_code,telegram_receipt=NULLIF(@receipt::jsonb,'null'::jsonb),
 telegram_effect_started_at=CASE WHEN @retry::boolean THEN NULL ELSE telegram_effect_started_at END,
 available_at=@available_at,lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 completed_at=CASE WHEN @retry::boolean THEN NULL ELSE clock_timestamp() END,version=version+1,updated_at=clock_timestamp()
WHERE id=@delivery_id::uuid;
