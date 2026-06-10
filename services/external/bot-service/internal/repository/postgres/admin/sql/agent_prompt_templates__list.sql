-- name: agent_prompt_templates__list :many
select id, profile_name, template_key, body, created_at, updated_at
from matter_codex_agent_prompt_templates
where profile_name = $1 or $1 = ''
order by profile_name, template_key;
