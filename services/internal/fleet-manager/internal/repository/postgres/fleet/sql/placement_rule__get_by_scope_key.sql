-- name: placement_rule__get_by_scope_key :one
SELECT
    id, fleet_scope_id, rule_key, status, priority,
    match_json, constraints_json, version, created_at, updated_at
FROM fleet_manager_placement_rules
WHERE fleet_scope_id = @fleet_scope_id
  AND rule_key = @rule_key;
