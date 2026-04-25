-- name: accessgraph__list_user_group_memberships :many
SELECT m.group_id, m.user_id, u.email
FROM user_group_memberships AS m
JOIN users AS u ON u.id = m.user_id
ORDER BY u.email ASC, m.group_id ASC
LIMIT $1;
