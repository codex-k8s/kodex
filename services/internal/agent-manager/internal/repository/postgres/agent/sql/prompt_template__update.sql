-- name: prompt_template__update :exec
UPDATE agent_manager_prompt_templates
SET
    active_version_id = @active_version_id::uuid,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
