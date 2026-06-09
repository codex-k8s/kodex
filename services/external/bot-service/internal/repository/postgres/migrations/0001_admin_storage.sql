create table if not exists matter_codex_repositories (
	id bigserial primary key,
	provider text not null,
	owner text not null,
	name text not null,
	default_branch text not null default 'main',
	status text not null default 'active',
	mattermost_channel text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (provider, owner, name)
);

create table if not exists matter_codex_credentials (
	id bigserial primary key,
	name text not null unique,
	credential_type text not null,
	provider text not null default '',
	secret_ref text not null default '',
	status text not null default 'unknown',
	last_checked_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists matter_codex_openai_accounts (
	id bigserial primary key,
	name text not null unique,
	credential_id bigint references matter_codex_credentials(id),
	status text not null default 'not_authorized',
	model_policy text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists matter_codex_agent_profiles (
	id bigserial primary key,
	name text not null unique,
	role text not null,
	description text not null default '',
	enabled boolean not null default true,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

insert into matter_codex_agent_profiles(name, role, description)
values
	('developer', 'developer', 'Codex developer agent profile seed'),
	('reviewer', 'reviewer', 'Codex reviewer agent profile seed'),
	('manager', 'manager', 'Mattermost manager profile seed')
on conflict (name) do nothing;

create table if not exists matter_codex_audit_events (
	id bigserial primary key,
	event_type text not null,
	actor_user_id text not null default '',
	actor_user text not null default '',
	resource_type text not null default '',
	resource_name text not null default '',
	summary text not null default '',
	created_at timestamptz not null default now()
);
