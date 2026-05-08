-- name: job__claim :one
WITH candidate AS (
    SELECT id
    FROM runtime_manager_jobs
    WHERE (
            status = 'pending'
            OR (status IN ('claimed', 'running') AND lease_until <= @now)
        )
      AND (cardinality(@job_types::text[]) = 0 OR job_type = ANY(@job_types::text[]))
      AND (@fleet_scope_id::uuid IS NULL OR fleet_scope_id = @fleet_scope_id::uuid)
    ORDER BY
        CASE priority
            WHEN 'blocking' THEN 4
            WHEN 'high' THEN 3
            WHEN 'normal' THEN 2
            ELSE 1
        END DESC,
        created_at ASC,
        id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE runtime_manager_jobs j
SET
    status = 'claimed',
    lease_owner = @lease_owner,
    lease_token_hash = @lease_token_hash,
    lease_until = @lease_until,
    claim_attempt = j.claim_attempt + 1,
    started_at = COALESCE(j.started_at, @now),
    updated_at = @now,
    version = j.version + 1
FROM candidate
WHERE j.id = candidate.id
RETURNING
    j.id,
    j.command_id,
    j.job_type,
    j.status,
    j.priority,
    j.job_input_json,
    j.lease_owner,
    j.lease_token_hash,
    j.lease_until,
    j.claim_attempt,
    j.slot_id,
    j.agent_run_id,
    j.project_id,
    j.repository_id,
    j.release_line_id,
    j.package_installation_id,
    j.fleet_scope_id,
    j.cluster_id,
    j.requested_by,
    j.created_at,
    j.started_at,
    j.finished_at,
    j.next_action,
    j.last_error_code,
    j.last_error_message,
    j.short_log_tail,
    j.full_log_ref,
    j.updated_at,
    j.version;
