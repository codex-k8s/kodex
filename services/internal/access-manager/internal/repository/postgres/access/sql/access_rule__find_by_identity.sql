-- name: access_rule__find_by_identity :one
SELECT id, effect, subject_type, subject_id, action_key, resource_type, resource_id,
       scope_type, scope_id, priority, status, version, created_at, updated_at
FROM access_rules
WHERE effect = @effect
  AND subject_type = @subject_type
  AND subject_id = @subject_id
  AND action_key = @action_key
  AND resource_type = @resource_type
  AND resource_id = @resource_id
  AND scope_type = @scope_type
  AND scope_id = @scope_id;
