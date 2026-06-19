-- +goose Up
create table if not exists matter_codex_project_runtime_variables (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	name text not null,
	slug text not null,
	description text not null default '',
	secret_ref text not null,
	secret_key text not null default 'value',
	sensitive boolean not null default true,
	enabled boolean not null default true,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (project_id, name),
	unique (project_id, slug),
	check (name ~ '^[A-Z][A-Z0-9_]{1,127}$'),
	check (slug ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')
);

create index if not exists matter_codex_project_runtime_variables_project_idx
	on matter_codex_project_runtime_variables(project_id, enabled desc, name);

create table if not exists matter_codex_agent_role_runtime_variables (
	id bigserial primary key,
	role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	variable_id bigint not null references matter_codex_project_runtime_variables(id) on delete cascade,
	created_at timestamptz not null default now(),
	unique (role_id, variable_id)
);

create index if not exists matter_codex_agent_role_runtime_variables_role_idx
	on matter_codex_agent_role_runtime_variables(role_id, created_at);

-- +goose Down
drop table if exists matter_codex_agent_role_runtime_variables;
drop table if exists matter_codex_project_runtime_variables;
