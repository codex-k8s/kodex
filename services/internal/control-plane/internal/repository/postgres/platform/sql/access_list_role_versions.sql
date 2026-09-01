-- name: access_list_role_versions :many
SELECT rv.ref, rv.revision, rv.name, rv.description, rv.permission_keys,
       rv.allowed_scopes, rv.change_comment, rv.created_at,
       creator.ref, creator.display_name, creator.email_masked
FROM control_plane.application_role_versions rv
JOIN control_plane.application_roles r ON r.id = rv.role_id
JOIN control_plane.subjects creator ON creator.id = rv.created_by
WHERE r.organization_id = @organization_id::uuid AND r.ref = @role_ref
  AND (@cursor::bigint = 0 OR rv.revision < @cursor::bigint)
ORDER BY rv.revision DESC
LIMIT @limit
