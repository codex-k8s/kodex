-- name: prompt_template_version__create :exec
INSERT INTO agent_manager_prompt_template_versions (
    id, prompt_template_id, role_profile_id, prompt_kind, version, source_ref,
    template_object_uri, template_object_digest, template_object_size_bytes,
    template_digest, status, activated_at, created_at
) VALUES (
    @id, @prompt_template_id, @role_profile_id, @prompt_kind, @version, @source_ref,
    @template_object_uri, @template_object_digest, @template_object_size_bytes,
    @template_digest, @status, @activated_at::timestamptz, @created_at
);
