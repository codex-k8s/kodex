-- name: platform__proof_project_authorize_membership :one
SELECT p.id::text, p.version
FROM control_plane.projects p
WHERE p.ref = $1 AND p.organization_id = $2::uuid AND p.lifecycle = 'ACTIVE'
  AND EXISTS (
      SELECT 1 FROM control_plane.memberships m
      WHERE m.organization_id = p.organization_id
        AND m.subject_id = $3::uuid
        AND m.active
        AND (m.project_id IS NULL OR m.project_id = p.id)
  )
