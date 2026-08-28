-- name: proof_system_resolve_identity :one
SELECT s.id::text, o.id::text, s.updated_at, o.version
FROM control_plane.subjects s
JOIN control_plane.organizations o ON o.id = s.organization_id
WHERE s.issuer = 'kodex-system'
  AND s.ref = 'sys_platform'
  AND s.active
  AND EXISTS (
      SELECT 1
      FROM control_plane.access_bindings binding
      JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
      JOIN control_plane.application_roles role ON role.id = role_version.role_id
      WHERE binding.organization_id = o.id
        AND binding.subject_id = s.id
        AND binding.scope_kind = 'ORGANIZATION'
        AND binding.state = 'ACTIVE'
        AND role.kind = 'SYSTEM'
        AND role.stable_key = 'OWNER'
  )
LIMIT 1
