-- name: telegram_begin_defer :exec
UPDATE control_plane.interaction_deliveries
SET state=CASE WHEN @cancel::boolean THEN 'CANCELLED' ELSE 'DUE' END,
 safe_error_code=@safe_error_code,available_at=@available_at,
 attempt=CASE WHEN @cancel::boolean THEN attempt ELSE greatest(0,attempt-1) END,
 lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 completed_at=CASE WHEN @cancel::boolean THEN clock_timestamp() ELSE NULL END,
 version=version+1,updated_at=clock_timestamp()
WHERE id=@delivery_id::uuid AND telegram_effect_started_at IS NULL;
