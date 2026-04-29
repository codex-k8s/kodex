-- name: access_rule__insert :exec
INSERT INTO access_rules (
    id, effect, subject_type, subject_id, action_key, resource_type, resource_id,
    scope_type, scope_id, priority, status, version, created_at, updated_at
) VALUES (
    @id, @effect, @subject_type, @subject_id, @action_key, @resource_type, @resource_id,
    @scope_type, @scope_id, @priority, @status, @version, @created_at, @updated_at
);
