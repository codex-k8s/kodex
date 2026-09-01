-- name: proof_project_authorize_membership :one
SELECT p.id::text, p.version
FROM control_plane.projects p
WHERE p.ref = $1 AND p.organization_id = $2::uuid AND p.lifecycle = 'ACTIVE'
  AND EXISTS (
    SELECT 1
    FROM control_plane.access_bindings b
    JOIN control_plane.application_role_versions rv ON rv.id = b.role_version_id
    WHERE b.organization_id = p.organization_id
      AND b.state = 'ACTIVE'
      AND 'project.view' = ANY(rv.permission_keys)
      AND (b.valid_from IS NULL OR b.valid_from <= clock_timestamp())
      AND (b.valid_until IS NULL OR b.valid_until > clock_timestamp())
      AND NOT b.require_owner
      AND (
        b.subject_id = $3::uuid OR
        b.oidc_group_id IN (
          SELECT gm.group_id FROM control_plane.oidc_group_memberships gm
          WHERE gm.organization_id = p.organization_id AND gm.subject_id = $3::uuid
        )
      )
      AND (
        b.scope_kind = 'ORGANIZATION' OR
        (b.scope_kind = 'PROJECT' AND b.project_id = p.id) OR
        (b.scope_kind = 'RESOURCE_KIND' AND b.resource_kind = 'PROJECT' AND (b.project_id IS NULL OR b.project_id = p.id)) OR
        (b.scope_kind = 'RESOURCE_INSTANCE' AND b.resource_kind = 'PROJECT' AND b.resource_id = p.id)
      )
  )
