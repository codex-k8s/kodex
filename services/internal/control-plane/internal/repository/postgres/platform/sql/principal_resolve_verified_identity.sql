-- name: principal_resolve_verified_identity :one
SELECT s.ref, o.ref
FROM control_plane.subjects s
JOIN control_plane.organizations o ON o.id = s.organization_id
WHERE s.id = $1::uuid
  AND o.id = $2::uuid
  AND s.active
