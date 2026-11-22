-- name: kubernetes_invocation__cursor_advance :exec
UPDATE control_plane.kubernetes_invocation_claim_cursors
SET after_created_at=@after_created_at,after_ref=@after_ref,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid;
