-- name: placement_rule__update :exec
UPDATE fleet_manager_placement_rules
SET fleet_scope_id = @fleet_scope_id,
    rule_key = @rule_key,
    status = @status,
    priority = @priority,
    match_json = @match_json,
    constraints_json = @constraints_json,
    updated_at = @updated_at,
    version = @version
WHERE id = @id
  AND version = @previous_version::bigint;
