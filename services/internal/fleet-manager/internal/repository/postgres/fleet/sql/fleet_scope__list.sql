-- name: fleet_scope__list :many
SELECT
    id, scope_key, scope_type, scope_owner_id, owner_ref_json, display_name,
    status, is_default, version, created_at, updated_at
FROM fleet_manager_scopes
WHERE (cardinality(@scope_types::text[]) = 0 OR scope_type = ANY(@scope_types::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@scope_owner_id::uuid IS NULL OR scope_owner_id = @scope_owner_id)
  AND (@is_default::boolean IS NULL OR is_default = @is_default)
ORDER BY scope_key, id
LIMIT @limit::integer OFFSET @offset::integer;
