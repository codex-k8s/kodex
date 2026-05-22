-- name: run__create :exec
INSERT INTO agent_manager_runs (
    id, session_id, flow_version_id, stage_id, role_profile_id,
    role_profile_version, role_profile_digest, prompt_template_version_id,
    prompt_template_digest, runtime_context, provider_target, guidance_refs,
    status, result_summary, failure_code, version, started_at, finished_at,
    created_at, updated_at
) VALUES (
    @id, @session_id, @flow_version_id::uuid, @stage_id::uuid, @role_profile_id,
    @role_profile_version, @role_profile_digest, @prompt_template_version_id,
    @prompt_template_digest, @runtime_context::jsonb, @provider_target::jsonb, @guidance_refs::jsonb,
    @status, @result_summary, @failure_code, @version, @started_at, @finished_at,
    @created_at, @updated_at
);
