-- name: prompt_template_version__list :many
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
WHERE role_profile_id = @role_profile_id
  AND (@prompt_kind::text IS NULL OR prompt_kind = @prompt_kind::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY prompt_kind, version DESC, id
LIMIT @limit::integer
OFFSET @offset::integer;
