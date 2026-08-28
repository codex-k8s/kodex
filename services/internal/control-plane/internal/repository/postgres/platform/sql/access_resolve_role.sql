-- name: access_resolve_role :one
SELECT r.id::text, r.kind, r.state, r.version, rv.revision
FROM control_plane.application_roles r
JOIN control_plane.application_role_versions rv ON rv.id = r.current_version_id
WHERE r.organization_id = @organization_id::uuid AND r.ref = @role_ref
FOR UPDATE OF r
