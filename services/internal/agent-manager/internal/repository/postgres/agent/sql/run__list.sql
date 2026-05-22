-- name: run__list :many
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
WHERE (@session_id::uuid IS NULL OR session_id = @session_id::uuid)
  AND (@role_profile_id::uuid IS NULL OR role_profile_id = @role_profile_id::uuid)
  AND (@status::text IS NULL OR status = @status::text)
  AND (@provider_work_item_ref::text IS NULL OR provider_target->>'work_item_ref' = @provider_work_item_ref::text)
ORDER BY updated_at DESC, id DESC
LIMIT @limit::int
OFFSET @offset::int;
