-- name: prewarm_pool__count_slots :one
SELECT count(*)::bigint
FROM runtime_manager_slots
WHERE status = 'prewarmed'
  AND is_prewarmed = true
  AND runtime_profile = @runtime_profile
  AND fleet_scope_id IS NOT DISTINCT FROM @fleet_scope_id::uuid
  AND (
      @scope_type = 'platform'
      OR (@scope_type = 'project' AND project_id::text = @scope_id)
      OR (@scope_type = 'repository' AND repository_ids_json ? @scope_id)
  );
