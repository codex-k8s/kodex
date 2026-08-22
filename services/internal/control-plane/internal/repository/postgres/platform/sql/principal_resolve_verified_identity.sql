-- name: platform__principal_resolve_verified_identity :one
SELECT s.ref, o.ref
FROM control_plane.subjects s
JOIN control_plane.organizations o ON o.id = s.organization_id
WHERE s.id = $1::uuid
  AND o.id = $2::uuid
  AND s.active
  AND EXISTS (
      SELECT 1 FROM control_plane.memberships m
      WHERE m.organization_id = o.id
        AND m.subject_id = s.id
        AND m.project_id IS NULL
        AND m.active
  )
