-- name: access_change_binding :one
UPDATE control_plane.access_bindings
SET role_version_id = @role_version_id::uuid,
    scope_kind = @scope_kind,
    project_id = NULLIF(@project_id, '')::uuid,
    resource_kind = NULLIF(@resource_kind, ''),
    resource_id = NULLIF(@resource_id, '')::uuid,
    valid_from = @valid_from,
    valid_until = @valid_until,
    require_owner = @require_owner,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND ref = @binding_ref
  AND state = 'ACTIVE' AND version = @expected_version
RETURNING id::text, ref, version, state, created_at, updated_at
