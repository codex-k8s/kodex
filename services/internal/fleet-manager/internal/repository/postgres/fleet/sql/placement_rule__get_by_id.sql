-- name: placement_rule__get_by_id :one
SELECT
    id, fleet_scope_id, rule_key, status, priority,
    match_json, constraints_json, version, created_at, updated_at
FROM fleet_manager_placement_rules
WHERE id = @id;
