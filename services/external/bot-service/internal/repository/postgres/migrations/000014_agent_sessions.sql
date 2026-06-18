-- +goose Up
create table if not exists matter_codex_mattermost_bot_identities (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	username text not null,
	display_name text not null default '',
	mattermost_user_id text not null default '',
	token_secret_ref text not null default '',
	status text not null default 'pending',
	last_error text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (project_id, username),
	unique (role_id)
);

create index if not exists matter_codex_bot_identities_user_idx
	on matter_codex_mattermost_bot_identities(mattermost_user_id)
	where mattermost_user_id <> '';

create table if not exists matter_codex_agent_sessions (
	id bigserial primary key,
	session_key text not null unique,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	chat_id bigint not null references matter_codex_chats(id) on delete cascade,
	role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	session_scope text not null,
	mattermost_channel_id text not null default '',
	mattermost_root_post_id text not null default '',
	codex_session_id text not null default '',
	status text not null default 'idle',
	active_turn_id bigint,
	active_run_id text not null default '',
	kubernetes_namespace text not null default '',
	pod_name text not null default '',
	pvc_name text not null default '',
	token_secret_ref text not null default '',
	capabilities jsonb not null default '{}'::jsonb,
	session_archive_gzip_base64 text not null default '',
	ttl_seconds integer not null,
	last_activity_at timestamptz not null default now(),
	expires_at timestamptz not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create index if not exists matter_codex_agent_sessions_chat_idx
	on matter_codex_agent_sessions(chat_id, session_scope, updated_at desc);

create index if not exists matter_codex_agent_sessions_thread_idx
	on matter_codex_agent_sessions(chat_id, mattermost_root_post_id, role_id)
	where mattermost_root_post_id <> '';

create index if not exists matter_codex_agent_sessions_expiry_idx
	on matter_codex_agent_sessions(status, expires_at);

create table if not exists matter_codex_agent_session_turns (
	id bigserial primary key,
	session_id bigint not null references matter_codex_agent_sessions(id) on delete cascade,
	run_id text not null unique,
	mattermost_channel_id text not null,
	mattermost_root_post_id text not null,
	mattermost_post_id text not null,
	user_id text not null default '',
	user_name text not null default '',
	message text not null,
	status text not null default 'queued',
	final_message text not null default '',
	error_message text not null default '',
	artifacts jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	started_at timestamptz,
	finished_at timestamptz,
	updated_at timestamptz not null default now()
);

create index if not exists matter_codex_agent_session_turns_queue_idx
	on matter_codex_agent_session_turns(session_id, status, created_at);

create index if not exists matter_codex_agent_session_turns_post_idx
	on matter_codex_agent_session_turns(mattermost_post_id);

-- +goose Down
drop table if exists matter_codex_agent_session_turns;
drop table if exists matter_codex_agent_sessions;
drop table if exists matter_codex_mattermost_bot_identities;
