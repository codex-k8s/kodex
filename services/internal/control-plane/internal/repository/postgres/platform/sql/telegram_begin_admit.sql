-- name: telegram_begin_admit :exec
WITH reserved AS (
 UPDATE control_plane.telegram_subscriptions
 SET used_effects=used_effects+1,
  rate_window_effects=CASE WHEN rate_window_started_at+interval '1 minute'<=@now THEN 1 ELSE rate_window_effects+1 END,
  rate_window_started_at=CASE WHEN rate_window_started_at+interval '1 minute'<=@now THEN @now ELSE rate_window_started_at END
 WHERE id=@subscription_id::uuid AND enabled AND used_effects<max_effects
 RETURNING id
)
UPDATE control_plane.interaction_deliveries
SET telegram_effect_started_at=@now,version=version+1,updated_at=@now
WHERE id=@delivery_id::uuid AND telegram_effect_started_at IS NULL AND telegram_subscription_id IN (SELECT id FROM reserved);
