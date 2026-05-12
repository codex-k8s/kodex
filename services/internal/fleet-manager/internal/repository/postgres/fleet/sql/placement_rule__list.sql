-- name: placement_rule__list :many
SELECT
    id, fleet_scope_id, rule_key, status, priority,
    match_json, constraints_json, version, created_at, updated_at
FROM fleet_manager_placement_rules
WHERE (@fleet_scope_id::uuid IS NULL OR fleet_scope_id = @fleet_scope_id::uuid)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY priority ASC, rule_key ASC, id ASC
LIMIT @limit::bigint
OFFSET @offset::bigint;
