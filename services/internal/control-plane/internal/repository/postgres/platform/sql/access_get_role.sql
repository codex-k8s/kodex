-- name: access_get_role :one
SELECT r.ref, r.kind, r.state, r.version, r.updated_at,
       rv.ref, rv.revision, rv.name, rv.description, rv.permission_keys,
       rv.allowed_scopes, rv.change_comment, rv.created_at,
       creator.ref, creator.display_name, creator.email_masked,
       count(DISTINCT b.id) FILTER (WHERE b.state = 'ACTIVE')::integer
FROM control_plane.application_roles r
JOIN control_plane.application_role_versions rv ON rv.id = r.current_version_id
JOIN control_plane.subjects creator ON creator.id = rv.created_by
LEFT JOIN control_plane.access_bindings b ON b.role_version_id = rv.id
WHERE r.organization_id = @organization_id::uuid AND r.ref = @role_ref
GROUP BY r.id, rv.id, creator.id
