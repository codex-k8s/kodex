-- name: interaction_approval_update :exec
UPDATE control_plane.interaction_deliveries
SET state=@next_state,safe_error_code=@safe_error_code,
    completed_at=CASE WHEN @next_state='CANCELLED' THEN clock_timestamp() ELSE NULL END,
    version=version+1,updated_at=clock_timestamp()
WHERE id=@delivery_id::uuid AND state='WAITING_APPROVAL';
