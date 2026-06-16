-- name: agent_profiles__get :one
select id, name, role, description, enabled, openai_account_name, github_account_name, kubernetes_access, sandbox_mode, config_overlay, created_at, updated_at
from matter_codex_agent_profiles
where name = $1;
