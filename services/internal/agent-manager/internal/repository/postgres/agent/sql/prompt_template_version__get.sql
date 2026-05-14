-- name: prompt_template_version__get :one
SELECT
    id,
    prompt_template_id,
    role_profile_id,
    prompt_kind,
    version,
    source_ref,
    template_object_uri,
    template_object_digest,
    template_object_size_bytes,
    template_digest,
    status,
    activated_at,
    created_at
FROM agent_manager_prompt_template_versions
WHERE id = @id;
