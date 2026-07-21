-- name: agent_prompt_templates__upgrade_seed :exec
update matter_codex_agent_prompt_templates
set body = $4,
	updated_at = now()
where profile_name = $1
	and template_key = $2
	and body = $3;
