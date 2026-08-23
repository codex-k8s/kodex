-- name: proof_owner_check_membership :one
SELECT COALESCE(bool_or(active), false)
FROM control_plane.memberships
WHERE organization_id = $1::uuid
  AND subject_id = $2::uuid
  AND project_id IS NULL
