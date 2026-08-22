-- name: platform__commands_changemembership_insert_memberships_ref_project_id_role :one
INSERT INTO control_plane.memberships
    (ref, organization_id, project_id, subject_id, role, permissions, active)
SELECT $1, $2::uuid, $3::uuid, subject.id, $5, $6, true
FROM control_plane.subjects subject
WHERE subject.organization_id = $2::uuid
  AND subject.ref = $4
  AND subject.active
  AND subject.issuer = 'verified-oidc-subject'
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.memberships platform_membership
      WHERE platform_membership.organization_id = $2::uuid
        AND platform_membership.subject_id = subject.id
        AND platform_membership.project_id IS NULL
        AND platform_membership.active
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.memberships project_membership
      WHERE project_membership.organization_id = $2::uuid
        AND project_membership.project_id = $3::uuid
        AND project_membership.subject_id = subject.id
  )
RETURNING ref, role, permissions, active, version;
