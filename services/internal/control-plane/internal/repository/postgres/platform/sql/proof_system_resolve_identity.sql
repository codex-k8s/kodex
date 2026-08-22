-- name: proof_system_resolve_identity :one
SELECT s.id::text, o.id::text, s.updated_at, o.version
FROM control_plane.subjects s
JOIN control_plane.organizations o ON o.id = s.organization_id
WHERE s.issuer = 'mattercodex-system'
  AND s.ref = 'sys_platform'
  AND s.active
  AND EXISTS (
      SELECT 1 FROM control_plane.memberships m
      WHERE m.organization_id = o.id AND m.subject_id = s.id AND m.active
  )
LIMIT 1
