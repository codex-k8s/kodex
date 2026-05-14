-- name: prompt_template__list :many
SELECT
    id,
    role_profile_id,
    prompt_kind,
    active_version_id,
    version,
    created_at,
    updated_at
FROM agent_manager_prompt_templates
WHERE role_profile_id = @role_profile_id
  AND (@prompt_kind::text IS NULL OR prompt_kind = @prompt_kind::text)
ORDER BY prompt_kind, id
LIMIT @limit::integer
OFFSET @offset::integer;
