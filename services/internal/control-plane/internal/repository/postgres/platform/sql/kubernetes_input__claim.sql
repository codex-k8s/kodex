-- name: kubernetes_input__claim :exec
UPDATE control_plane.kubernetes_input_stages SET state='CLAIMED',delivery_expires_at=@delivery_expires_at,claimed_at=clock_timestamp()
WHERE id=@stage_id::uuid AND organization_id=@organization_id::uuid AND state='QUEUED' AND @delivery_expires_at>clock_timestamp();
