-- name: prompt_template_version__supersede :exec
UPDATE agent_manager_prompt_template_versions
SET status = 'superseded'
WHERE prompt_template_id = @prompt_template_id
  AND id <> @id
  AND status = 'active';
