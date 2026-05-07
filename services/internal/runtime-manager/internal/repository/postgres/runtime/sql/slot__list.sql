-- name: slot__list :many
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
WHERE (@project_id::uuid IS NULL OR project_id = @project_id::uuid)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@runtime_profile = '' OR runtime_profile = @runtime_profile)
  AND (@fleet_scope_id::uuid IS NULL OR fleet_scope_id = @fleet_scope_id::uuid)
  AND (@agent_run_id::uuid IS NULL OR agent_run_id = @agent_run_id::uuid)
ORDER BY updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
