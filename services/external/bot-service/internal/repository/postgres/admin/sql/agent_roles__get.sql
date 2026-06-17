-- name: agent_roles__get :one
select id, project_id, name, role_type, description, coalesce(prompt_template, ''), prompt_mode, github_account_name, openai_account_name, kubernetes_access, sandbox_mode, config_overlay, advanced_settings::text, enabled, bot_identity, created_at, updated_at
from matter_codex_agent_roles
where id = $1;
