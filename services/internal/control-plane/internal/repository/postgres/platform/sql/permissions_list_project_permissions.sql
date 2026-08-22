-- name: permissions_list_project_permissions :one
SELECT COALESCE(array_agg(DISTINCT permission ORDER BY permission), '{}'::text[])
FROM control_plane.memberships membership
CROSS JOIN LATERAL unnest(membership.permissions) permission
WHERE membership.organization_id=$1::uuid
  AND membership.project_id=$2::uuid
  AND membership.subject_id=$3::uuid
  AND membership.active
