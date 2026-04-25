-- name: accessgraph__list_organization_memberships :many
SELECT m.organization_id, m.user_id, u.email, m.role
FROM organization_memberships AS m
JOIN users AS u ON u.id = m.user_id
ORDER BY u.email ASC, m.organization_id ASC
LIMIT $1;
