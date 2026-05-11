-- name: prewarm_pool__list_excess_slots :many
SELECT
    id,
    slot_key,
    status,
    runtime_mode,
    is_prewarmed,
    fleet_scope_id,
    cluster_id,
    namespace_name,
    agent_run_id,
    project_id,
    repository_ids_json,
    active_workspace_materialization_id,
    runtime_profile,
    fingerprint,
    lease_owner,
    lease_until,
    last_error_code,
    last_error_message,
    version,
    created_at,
    updated_at
FROM runtime_manager_slots
WHERE status = 'prewarmed'
  AND is_prewarmed = true
  AND runtime_profile = @runtime_profile
  AND fleet_scope_id IS NOT DISTINCT FROM @fleet_scope_id::uuid
  AND (
      @scope_type = 'platform'
      OR (@scope_type = 'project' AND project_id::text = @scope_id)
      OR (@scope_type = 'repository' AND repository_ids_json ? @scope_id)
  )
ORDER BY updated_at DESC, id DESC
FOR UPDATE SKIP LOCKED
LIMIT @limit;
