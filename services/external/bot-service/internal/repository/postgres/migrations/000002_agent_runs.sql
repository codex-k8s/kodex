-- +goose Up
create table if not exists matter_codex_agent_runs (
	id bigserial primary key,
	run_id text not null unique,
	profile_name text not null,
	role text not null,
	provider text not null,
	owner text not null,
	name text not null,
	base_branch text not null default 'main',
	head_branch text not null,
	status text not null default 'started',
	kubernetes_namespace text not null default '',
	job_name text not null default '',
	pvc_name text not null default '',
	pr_url text not null default '',
	summary text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create index if not exists matter_codex_agent_runs_repo_created_idx
	on matter_codex_agent_runs(provider, owner, name, created_at desc);

-- +goose Down
drop table if exists matter_codex_agent_runs;
