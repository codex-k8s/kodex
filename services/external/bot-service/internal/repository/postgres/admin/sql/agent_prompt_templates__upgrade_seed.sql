-- name: agent_prompt_templates__upgrade_seed :one
with updated_template as (
	update matter_codex_agent_prompt_templates
	set body = $4,
		updated_at = now()
	where profile_name = $1
		and template_key = $2
		and body = $3
	returning 1
),
updated_roles as (
	update matter_codex_agent_roles
	set prompt_template = $4,
		updated_at = now()
	where prompt_template = $3
		and lower(btrim(kubernetes_access)) <> 'cluster-admin'
		and (
			lower(btrim(name)) = any($5::text[])
			or lower(btrim(role_type)) = any($6::text[])
		)
	returning 1
)
select
	(select count(*) from updated_template),
	(select count(*) from updated_roles);
