-- +goose Up
create table if not exists matter_codex_agent_prompt_templates (
	id bigserial primary key,
	profile_name text not null,
	template_key text not null,
	body text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (profile_name, template_key)
);

create index if not exists matter_codex_agent_prompt_templates_profile_idx
	on matter_codex_agent_prompt_templates(profile_name);

-- Default prompt bodies are seeded from embedded Markdown files by bot-service
-- startup code. SQL migrations own schema only, so prompt text stays reviewable
-- as normal Markdown.

-- +goose Down
drop table if exists matter_codex_agent_prompt_templates;
