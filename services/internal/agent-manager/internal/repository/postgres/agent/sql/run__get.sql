-- name: run__get :one
SELECT
    id,
    session_id,
    flow_version_id,
    stage_id,
    role_profile_id,
    role_profile_version,
    role_profile_digest,
    prompt_template_version_id,
    prompt_template_digest,
    runtime_context,
    provider_target,
    guidance_refs,
    status,
    result_summary,
    failure_code,
    version,
    started_at,
    finished_at,
    created_at,
    updated_at
FROM agent_manager_runs
WHERE id = @id;
