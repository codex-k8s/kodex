-- name: access_rule__list_for_check :many
WITH candidate_subjects AS (
    SELECT subject_type, subject_id
    FROM unnest(@subject_types::text[], @subject_ids::text[]) AS t(subject_type, subject_id)
)
SELECT id, effect, subject_type, subject_id, action_key, resource_type, resource_id, scope_type, scope_id, priority, status, version, created_at, updated_at
FROM access_rules
WHERE (subject_type, subject_id) IN (SELECT subject_type, subject_id FROM candidate_subjects)
  AND action_key = @action_key
  AND resource_type = @resource_type
  AND (resource_id = '' OR resource_id = @resource_id)
  AND ((scope_type = 'global' AND scope_id = '') OR (scope_type = @scope_type AND scope_id = @scope_id))
  AND status = 'active'
ORDER BY priority DESC, updated_at DESC;
