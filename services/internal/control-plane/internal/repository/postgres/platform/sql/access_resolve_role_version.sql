-- name: access_resolve_role_version :one
SELECT rv.id::text, rv.ref, r.ref, rv.revision, rv.name, rv.description,
       rv.permission_keys, rv.allowed_scopes, rv.change_comment, rv.created_at,
       creator.ref, creator.display_name, creator.email_masked
FROM control_plane.application_role_versions rv
JOIN control_plane.application_roles r ON r.id = rv.role_id
JOIN control_plane.subjects creator ON creator.id = rv.created_by
WHERE rv.organization_id = @organization_id::uuid AND rv.ref = @role_version_ref
