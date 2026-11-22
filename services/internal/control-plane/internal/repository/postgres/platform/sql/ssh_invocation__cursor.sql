-- name: ssh_invocation__cursor :one
INSERT INTO control_plane.ssh_invocation_claim_cursors(organization_id) VALUES(@organization_id::uuid)
ON CONFLICT(organization_id) DO UPDATE SET organization_id=EXCLUDED.organization_id
RETURNING after_created_at,after_ref;
