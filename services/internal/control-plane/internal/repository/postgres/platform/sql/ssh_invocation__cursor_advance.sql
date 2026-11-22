-- name: ssh_invocation__cursor_advance :exec
UPDATE control_plane.ssh_invocation_claim_cursors SET after_created_at=@after_created_at,after_ref=@after_ref
WHERE organization_id=@organization_id::uuid;
