-- name: prompt_template_version__activate :exec
UPDATE agent_manager_prompt_template_versions
SET status = @status,
    activated_at = @activated_at::timestamptz
WHERE id = @id;
