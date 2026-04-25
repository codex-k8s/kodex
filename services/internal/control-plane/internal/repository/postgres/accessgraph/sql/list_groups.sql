-- name: accessgraph__list_groups :many
SELECT id, organization_id, scope, slug, name
FROM user_groups
ORDER BY scope ASC, name ASC, slug ASC
LIMIT $1;
