-- name: slot__claim_reusable :one
WITH candidate AS (
    SELECT id
    FROM runtime_manager_slots
    WHERE status IN ('prewarmed', 'ready')
      AND runtime_profile = @runtime_profile
      AND runtime_mode = @runtime_mode
      AND fleet_scope_id IS NOT DISTINCT FROM @fleet_scope_id::uuid
      AND (lease_until IS NULL OR lease_until <= @now::timestamptz)
      AND (project_id IS NULL OR project_id IS NOT DISTINCT FROM @project_id::uuid)
      AND (
          jsonb_array_length(repository_ids_json) = 0
          OR repository_ids_json <@ @repository_ids_json::jsonb
      )
      AND (
          (status = 'prewarmed' AND (fingerprint = '' OR fingerprint = @fingerprint))
          OR (status = 'ready' AND fingerprint = @fingerprint)
      )
      AND NOT EXISTS (
          SELECT 1
          FROM runtime_manager_jobs j
          WHERE j.slot_id = runtime_manager_slots.id
            AND j.status IN ('pending', 'claimed', 'running')
      )
    ORDER BY is_prewarmed DESC, updated_at ASC, id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE runtime_manager_slots s
SET
    status = 'reserved',
    cluster_id = @cluster_id::uuid,
    agent_run_id = @agent_run_id::uuid,
    project_id = @project_id::uuid,
    repository_ids_json = @repository_ids_json::jsonb,
    fingerprint = @fingerprint,
    lease_owner = @lease_owner,
    lease_until = @lease_until::timestamptz,
    last_error_code = '',
    last_error_message = '',
    version = s.version + 1,
    updated_at = @now::timestamptz
FROM candidate
WHERE s.id = candidate.id
RETURNING
    s.id,
    s.slot_key,
    s.status,
    s.runtime_mode,
    s.is_prewarmed,
    s.fleet_scope_id,
    s.cluster_id,
    s.namespace_name,
    s.agent_run_id,
    s.project_id,
    s.repository_ids_json,
    s.active_workspace_materialization_id,
    s.runtime_profile,
    s.fingerprint,
    s.lease_owner,
    s.lease_until,
    s.last_error_code,
    s.last_error_message,
    s.version,
    s.created_at,
    s.updated_at;
