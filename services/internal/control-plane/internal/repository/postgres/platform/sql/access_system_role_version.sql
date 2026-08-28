-- name: access_system_role_version :one
SELECT rv.id::text, rv.ref
FROM control_plane.application_roles r
JOIN control_plane.application_role_versions rv ON rv.id = r.current_version_id
WHERE r.organization_id = @organization_id::uuid AND r.kind = 'SYSTEM' AND r.stable_key = @stable_key
