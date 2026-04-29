-- name: organization__count_active_owner :one
SELECT count(*)::bigint
FROM access_organizations
WHERE kind = 'owner' AND status = 'active';
