-- +goose Up
create table if not exists matter_codex_github_accounts (
	id bigserial primary key,
	name text not null unique,
	credential_id bigint references matter_codex_credentials(id),
	secret_ref text not null default '',
	username text not null default '',
	email text not null default '',
	status text not null default 'unknown',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

alter table matter_codex_agent_profiles
	add column if not exists github_account_name text not null default 'primary';

create index if not exists matter_codex_agent_profiles_github_account_idx
	on matter_codex_agent_profiles(github_account_name);

insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
values
	('github:primary', 'github_token', 'github', 'matter-codex-github', 'configured'),
	('github:agent', 'github_token', 'github', 'matter-codex-github-agent', 'configured')
on conflict (name) do update set
	secret_ref = excluded.secret_ref,
	status = excluded.status,
	updated_at = now();

insert into matter_codex_github_accounts(name, credential_id, secret_ref, status)
select 'primary', id, 'matter-codex-github', 'configured'
from matter_codex_credentials
where name = 'github:primary'
on conflict (name) do update set
	credential_id = excluded.credential_id,
	secret_ref = excluded.secret_ref,
	status = excluded.status,
	updated_at = now();

insert into matter_codex_github_accounts(name, credential_id, secret_ref, status)
select 'agent', id, 'matter-codex-github-agent', 'configured'
from matter_codex_credentials
where name = 'github:agent'
on conflict (name) do update set
	credential_id = excluded.credential_id,
	secret_ref = excluded.secret_ref,
	status = excluded.status,
	updated_at = now();

update matter_codex_agent_profiles
set github_account_name = 'agent', updated_at = now()
where name = 'developer';

update matter_codex_agent_profiles
set github_account_name = 'primary', updated_at = now()
where name = 'reviewer';

-- Prompt bodies are maintained by the embedded Markdown seed catalog.

-- +goose Down
alter table matter_codex_agent_profiles
	drop column if exists github_account_name;

drop table if exists matter_codex_github_accounts;
