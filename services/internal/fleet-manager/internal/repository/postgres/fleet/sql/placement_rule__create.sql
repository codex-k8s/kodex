-- name: placement_rule__create :exec
INSERT INTO fleet_manager_placement_rules (
    id, fleet_scope_id, rule_key, status, priority,
    match_json, constraints_json, created_at, updated_at, version
) VALUES (
    @id, @fleet_scope_id, @rule_key, @status, @priority,
    @match_json, @constraints_json, @created_at, @updated_at, @version
);
