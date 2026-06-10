-- +goose Up
alter table matter_codex_agent_profiles
	add column if not exists openai_account_name text not null default 'primary';

create index if not exists matter_codex_agent_profiles_openai_account_idx
	on matter_codex_agent_profiles(openai_account_name);

insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
values ('openai:primary', 'codex_auth', 'openai', 'matter-codex-codex-auth-primary', 'not_authorized')
on conflict (name) do nothing;

insert into matter_codex_openai_accounts(name, credential_id, status)
select 'primary', id, 'not_authorized'
from matter_codex_credentials
where name = 'openai:primary'
on conflict (name) do nothing;

-- +goose Down
alter table matter_codex_agent_profiles
	drop column if exists openai_account_name;
