-- name: platform_membership__count_other_active_owners :one
SELECT count(*)
FROM control_plane.memberships
WHERE organization_id = @organization_id::uuid
  AND project_id IS NULL
  AND role = 'OWNER'
  AND active
  AND id <> @membership_id::uuid;
