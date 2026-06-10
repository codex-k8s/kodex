-- name: agent_prompt_templates__get :one
select id, profile_name, template_key, body, created_at, updated_at
from matter_codex_agent_prompt_templates
where profile_name = $1 and template_key = $2;
