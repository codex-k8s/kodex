-- name: access_insert_binding :one
INSERT INTO control_plane.access_bindings
    (ref, organization_id, subject_kind, subject_id, oidc_group_id, role_version_id,
     scope_kind, project_id, resource_kind, resource_id, valid_from, valid_until,
     require_owner, created_by)
VALUES (@ref, @organization_id::uuid, @subject_kind,
        NULLIF(@subject_id, '')::uuid, NULLIF(@oidc_group_id, '')::uuid,
        @role_version_id::uuid, @scope_kind, NULLIF(@project_id, '')::uuid,
        NULLIF(@resource_kind, ''), NULLIF(@resource_id, '')::uuid,
        @valid_from, @valid_until, @require_owner, @created_by::uuid)
RETURNING id::text, ref, version, state, created_at, updated_at
