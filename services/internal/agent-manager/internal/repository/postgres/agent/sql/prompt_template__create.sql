-- name: prompt_template__create :exec
INSERT INTO agent_manager_prompt_templates (
    id, role_profile_id, prompt_kind, active_version_id, version, created_at, updated_at
) VALUES (
    @id, @role_profile_id, @prompt_kind, @active_version_id::uuid, @version, @created_at, @updated_at
);
