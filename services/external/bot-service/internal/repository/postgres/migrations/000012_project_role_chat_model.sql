-- +goose Up
create table if not exists matter_codex_projects (
	id bigserial primary key,
	name text not null,
	slug text not null unique,
	mattermost_team_id text not null default '',
	description text not null default '',
	advanced_settings jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists matter_codex_project_repositories (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	repository_id bigint not null references matter_codex_repositories(id) on delete cascade,
	is_default boolean not null default false,
	metadata jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (project_id, repository_id)
);

create index if not exists matter_codex_project_repositories_project_idx
	on matter_codex_project_repositories(project_id, is_default desc, updated_at desc);

create table if not exists matter_codex_agent_roles (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	name text not null,
	role_type text not null,
	description text not null default '',
	prompt_template text,
	prompt_mode text not null default 'template',
	github_account_name text not null default '',
	openai_account_name text not null default '',
	kubernetes_access text not null default 'read-only',
	sandbox_mode text not null default 'danger-full-access',
	config_overlay text not null default '',
	advanced_settings jsonb not null default '{}'::jsonb,
	enabled boolean not null default true,
	bot_identity text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (project_id, name)
);

create index if not exists matter_codex_agent_roles_project_idx
	on matter_codex_agent_roles(project_id, role_type, name);

create table if not exists matter_codex_chats (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	mattermost_channel_id text not null default '',
	name text not null,
	slug text not null,
	description text not null default '',
	chat_type text not null default 'custom',
	root_github_issue text not null default '',
	work_policy text not null default '',
	settings jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (project_id, slug)
);

create index if not exists matter_codex_chats_project_idx
	on matter_codex_chats(project_id, updated_at desc);

create table if not exists matter_codex_chat_participants (
	id bigserial primary key,
	chat_id bigint not null references matter_codex_chats(id) on delete cascade,
	role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	enabled boolean not null default true,
	created_at timestamptz not null default now(),
	unique (chat_id, role_id)
);

create table if not exists matter_codex_chat_repositories (
	id bigserial primary key,
	chat_id bigint not null references matter_codex_chats(id) on delete cascade,
	repository_id bigint not null references matter_codex_repositories(id) on delete cascade,
	created_at timestamptz not null default now(),
	unique (chat_id, repository_id)
);

-- +goose Down
drop table if exists matter_codex_chat_repositories;
drop table if exists matter_codex_chat_participants;
drop table if exists matter_codex_chats;
drop table if exists matter_codex_agent_roles;
drop table if exists matter_codex_project_repositories;
drop table if exists matter_codex_projects;
