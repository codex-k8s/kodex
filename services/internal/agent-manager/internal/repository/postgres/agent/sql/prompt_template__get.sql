-- name: prompt_template__get :one
SELECT
    id,
    role_profile_id,
    prompt_kind,
    active_version_id,
    version,
    created_at,
    updated_at
FROM agent_manager_prompt_templates
WHERE id = @id;
