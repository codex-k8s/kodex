-- name: access_revoke_binding :one
UPDATE control_plane.access_bindings
SET state = 'REVOKED', version = version + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND ref = @binding_ref
  AND state = 'ACTIVE' AND version = @expected_version
RETURNING id::text, ref, version, state, created_at, updated_at
