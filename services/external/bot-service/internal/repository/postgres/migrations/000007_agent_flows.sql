-- +goose Up
alter table matter_codex_agent_runs
	add column if not exists flow_id text not null default '';

create index if not exists matter_codex_agent_runs_flow_idx
	on matter_codex_agent_runs(flow_id, created_at);

create table if not exists matter_codex_agent_flows (
	id bigserial primary key,
	flow_id text not null unique,
	status text not null default 'created',
	provider text not null default 'github',
	owner text not null,
	name text not null,
	base_branch text not null default 'main',
	head_branch text not null,
	title text not null default '',
	task text not null default '',
	pr_url text not null default '',
	pr_number integer not null default 0,
	attempt integer not null default 1,
	max_attempts integer not null default 3,
	current_developer_run_id text not null default '',
	current_reviewer_run_id text not null default '',
	summary text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create index if not exists matter_codex_agent_flows_repo_created_idx
	on matter_codex_agent_flows(provider, owner, name, created_at desc);

-- Developer flow prompt bodies are seeded from embedded Markdown files.

-- +goose Down
drop table if exists matter_codex_agent_flows;

drop index if exists matter_codex_agent_runs_flow_idx;

alter table matter_codex_agent_runs
	drop column if exists flow_id;
