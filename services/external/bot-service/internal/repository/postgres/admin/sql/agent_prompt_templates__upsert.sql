-- name: agent_prompt_templates__upsert :one
insert into matter_codex_agent_prompt_templates(profile_name, template_key, body)
values ($1, $2, $3)
on conflict (profile_name, template_key) do update set
	body = excluded.body,
	updated_at = now()
returning id, profile_name, template_key, body, created_at, updated_at, (xmax = 0) as created;
