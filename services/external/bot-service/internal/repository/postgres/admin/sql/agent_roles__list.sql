-- name: agent_roles__list :many
select id, project_id, name, role_type, description, coalesce(prompt_template, ''), prompt_mode, github_account_name, openai_account_name, kubernetes_access, sandbox_mode, config_overlay, advanced_settings::text, enabled, bot_identity, created_at, updated_at
from matter_codex_agent_roles
where ($1::bigint = 0 or project_id = $1)
order by project_id, role_type, name;
