-- name: accessgraph__list_organizations :many
SELECT id, slug, name
FROM organizations
ORDER BY name ASC, slug ASC
LIMIT $1;
