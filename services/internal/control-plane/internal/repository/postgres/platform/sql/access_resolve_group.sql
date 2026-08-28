-- name: access_resolve_group :one
SELECT g.id::text, g.ref, g.display_name, g.state = 'ACTIVE'
FROM control_plane.oidc_groups g
WHERE g.organization_id = @organization_id::uuid AND g.ref = @subject_ref
