-- name: cleanup_slot__claim_blocked :many
WITH candidate AS (
    SELECT s.id
    FROM runtime_manager_slots s
    WHERE (
        (s.status = 'cleanup_pending' AND s.updated_at <= @now::timestamptz - (@ttl_seconds::bigint * interval '1 second'))
        OR (s.status = 'failed' AND s.updated_at <= @now::timestamptz - (@failed_ttl_seconds::bigint * interval '1 second'))
    )
      AND (
        @scope_type = 'platform'
        OR (@scope_type = 'project' AND s.project_id::text = @scope_id)
        OR (@scope_type = 'repository' AND s.repository_ids_json ? @scope_id)
        OR (@scope_type = 'runtime_profile' AND s.runtime_profile = @scope_id)
      )
      AND EXISTS (
          SELECT 1
          FROM runtime_manager_jobs j
          WHERE j.slot_id = s.id
            AND j.status IN ('pending', 'claimed', 'running')
      )
    ORDER BY s.updated_at ASC, s.id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT @limit
)
UPDATE runtime_manager_slots s
SET
    last_error_code = 'CLEANUP_BLOCKED_BY_ACTIVE_JOB',
    last_error_message = 'cleanup is blocked by active runtime jobs',
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
