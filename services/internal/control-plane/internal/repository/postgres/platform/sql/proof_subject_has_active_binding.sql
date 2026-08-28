-- name: proof_subject_has_active_binding :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.access_bindings binding
    JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
    JOIN control_plane.application_roles role ON role.id = role_version.role_id
    WHERE binding.organization_id = $1::uuid
      AND binding.state = 'ACTIVE'
      AND role.state = 'ACTIVE'
      AND (binding.valid_from IS NULL OR binding.valid_from <= clock_timestamp())
      AND (binding.valid_until IS NULL OR binding.valid_until > clock_timestamp())
      AND (
          binding.subject_id = $2::uuid
          OR binding.oidc_group_id IN (
              SELECT membership.group_id
              FROM control_plane.oidc_group_memberships membership
              WHERE membership.organization_id = binding.organization_id
                AND membership.subject_id = $2::uuid
          )
      )
);
