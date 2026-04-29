-- name: access_action__get_by_key :one
SELECT id, key, display_name, description, resource_type, status, version, created_at, updated_at
FROM access_actions
WHERE key = @key;
