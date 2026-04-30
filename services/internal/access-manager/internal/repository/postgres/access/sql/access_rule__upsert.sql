-- name: access_rule__upsert :exec
INSERT INTO access_rules (
    id, effect, subject_type, subject_id, action_key, resource_type, resource_id,
    scope_type, scope_id, priority, status, version, created_at, updated_at
) VALUES (
    @id, @effect, @subject_type, @subject_id, @action_key, @resource_type, @resource_id,
    @scope_type, @scope_id, @priority, @status, @version, @created_at, @updated_at
)
ON CONFLICT (
    effect, subject_type, subject_id, action_key, resource_type, resource_id, scope_type, scope_id
) DO UPDATE SET
    priority = EXCLUDED.priority,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE access_rules.id = @id;
