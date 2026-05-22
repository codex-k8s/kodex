-- name: run__update :exec
UPDATE agent_manager_runs
SET
    session_id = @session_id,
    flow_version_id = @flow_version_id::uuid,
    stage_id = @stage_id::uuid,
    role_profile_id = @role_profile_id,
    role_profile_version = @role_profile_version,
    role_profile_digest = @role_profile_digest,
    prompt_template_version_id = @prompt_template_version_id,
    prompt_template_digest = @prompt_template_digest,
    runtime_context = @runtime_context::jsonb,
    provider_target = @provider_target::jsonb,
    guidance_refs = @guidance_refs::jsonb,
    status = @status,
    result_summary = @result_summary,
    failure_code = @failure_code,
    version = @version,
    started_at = @started_at,
    finished_at = @finished_at,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
