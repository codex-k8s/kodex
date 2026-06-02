-- name: job__claim :one
WITH runnable AS (
    SELECT
        j.*,
        j.job_input_json->'agent_run_execution_spec' AS agent_run_spec,
        j.job_input_json->'build_execution_spec' AS build_spec,
        j.job_input_json->'deploy_execution_spec' AS deploy_spec
    FROM runtime_manager_jobs j
),
candidate AS (
    SELECT id
    FROM runnable
    WHERE (
            status = 'pending'
            OR (status IN ('claimed', 'running') AND lease_until <= @now)
        )
      AND (cardinality(@job_types::text[]) = 0 OR job_type = ANY(@job_types::text[]))
      AND (@fleet_scope_id::uuid IS NULL OR fleet_scope_id = @fleet_scope_id::uuid)
      AND (job_type <> 'agent_run' OR jsonb_typeof(agent_run_spec) = 'object')
      AND (
            job_type <> 'build'
            OR (
                jsonb_typeof(build_spec) = 'object'
                AND jsonb_typeof(build_spec->'source_ref') = 'string'
                AND build_spec->>'source_ref' <> ''
                AND (build_spec->>'source_commit_sha') ~* '^([0-9a-f]{40}|[0-9a-f]{64})$'
                AND jsonb_typeof(build_spec->'service_key') = 'string'
                AND build_spec->>'service_key' <> ''
                AND jsonb_typeof(build_spec->'image_ref') = 'string'
                AND build_spec->>'image_ref' <> ''
                AND jsonb_typeof(build_spec->'image_tag') = 'string'
                AND build_spec->>'image_tag' <> ''
                AND (
                    build_spec->'image_digest' IS NULL
                    OR jsonb_typeof(build_spec->'image_digest') = 'null'
                    OR build_spec->>'image_digest' = ''
                    OR (build_spec->>'image_digest') ~* '^sha256:[0-9a-f]{64}$'
                )
                AND jsonb_typeof(build_spec->'build_context_ref') = 'string'
                AND build_spec->>'build_context_ref' <> ''
                AND (build_spec->>'build_context_digest') ~* '^sha256:[0-9a-f]{64}$'
                AND jsonb_typeof(build_spec->'dockerfile_ref') = 'string'
                AND build_spec->>'dockerfile_ref' <> ''
                AND (
                    build_spec->'dockerfile_digest' IS NULL
                    OR jsonb_typeof(build_spec->'dockerfile_digest') = 'null'
                    OR build_spec->>'dockerfile_digest' = ''
                    OR (build_spec->>'dockerfile_digest') ~* '^sha256:[0-9a-f]{64}$'
                )
                AND jsonb_typeof(build_spec->'dockerfile_target') = 'string'
                AND build_spec->>'dockerfile_target' <> ''
                AND jsonb_typeof(build_spec->'builder_image_ref') = 'string'
                AND build_spec->>'builder_image_ref' <> ''
                AND (build_spec->>'build_plan_fingerprint') ~* '^sha256:[0-9a-f]{64}$'
            )
        )
      AND (
            job_type <> 'deploy'
            OR (
                jsonb_typeof(deploy_spec) = 'object'
                AND jsonb_typeof(deploy_spec->'source_ref') = 'string'
                AND deploy_spec->>'source_ref' <> ''
                AND (deploy_spec->>'source_commit_sha') ~* '^([0-9a-f]{40}|[0-9a-f]{64})$'
                AND jsonb_typeof(deploy_spec->'service_key') = 'string'
                AND deploy_spec->>'service_key' <> ''
                AND jsonb_typeof(deploy_spec->'image_ref') = 'string'
                AND deploy_spec->>'image_ref' <> ''
                AND jsonb_typeof(deploy_spec->'image_tag') = 'string'
                AND deploy_spec->>'image_tag' <> ''
                AND (deploy_spec->>'image_digest') ~* '^sha256:[0-9a-f]{64}$'
                AND jsonb_typeof(deploy_spec->'manifest_ref') = 'string'
                AND deploy_spec->>'manifest_ref' <> ''
                AND (deploy_spec->>'manifest_digest') ~* '^sha256:[0-9a-f]{64}$'
                AND jsonb_typeof(deploy_spec->'kustomization_ref') = 'string'
                AND deploy_spec->>'kustomization_ref' <> ''
                AND (deploy_spec->>'kustomization_digest') ~* '^sha256:[0-9a-f]{64}$'
                AND (deploy_spec->>'target_namespace') ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'
                AND jsonb_typeof(deploy_spec->'target_cluster_ref') = 'string'
                AND deploy_spec->>'target_cluster_ref' <> ''
                AND (
                    deploy_spec->'target_slot_id' IS NULL
                    OR jsonb_typeof(deploy_spec->'target_slot_id') = 'null'
                    OR jsonb_typeof(deploy_spec->'target_slot_id') = 'string'
                )
                AND (deploy_spec->>'deploy_plan_fingerprint') ~* '^sha256:[0-9a-f]{64}$'
            )
        )
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
