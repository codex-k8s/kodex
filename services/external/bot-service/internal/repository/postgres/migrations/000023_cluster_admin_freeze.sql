-- +goose Up
create table if not exists matter_codex_cluster_admin_subjects (
	subject_type text not null,
	subject_key text not null,
	project_id bigint not null default 0,
	profile_name text not null,
	privilege_state jsonb not null,
	captured_at timestamptz not null default now(),
	primary key (subject_type, subject_key, project_id),
	constraint matter_codex_cluster_admin_subjects_type_check
		check (subject_type in ('agent_profile', 'agent_role'))
);

create table if not exists matter_codex_cluster_admin_revocations (
	resource_type text not null,
	resource_key text not null,
	reason text not null,
	revoked_at timestamptz not null default now(),
	primary key (resource_type, resource_key)
);

create table if not exists matter_codex_cluster_admin_bindings (
	role_id bigint not null,
	project_id bigint not null,
	chat_id bigint not null,
	mattermost_channel_id text not null,
	captured_at timestamptz not null default now(),
	primary key (role_id, chat_id)
);

create table if not exists matter_codex_cluster_admin_session_bindings (
	role_id bigint not null,
	project_id bigint not null,
	chat_id bigint not null,
	session_key text not null,
	mattermost_channel_id text not null,
	privilege_state jsonb not null,
	captured_at timestamptz not null default now(),
	primary key (role_id, session_key)
);

create table if not exists matter_codex_cluster_admin_bot_bindings (
	role_id bigint primary key,
	project_id bigint not null,
	privilege_state jsonb not null,
	captured_at timestamptz not null default now()
);

create table if not exists matter_codex_cluster_admin_runtime_variable_bindings (
	role_id bigint not null,
	variable_id bigint not null,
	privilege_state jsonb not null,
	captured_at timestamptz not null default now(),
	primary key (role_id, variable_id)
);

create table if not exists matter_codex_cluster_admin_dependencies (
	role_id bigint not null,
	resource_type text not null,
	resource_key text not null,
	privilege_state jsonb not null,
	captured_at timestamptz not null default now(),
	primary key (role_id, resource_type, resource_key),
	constraint matter_codex_cluster_admin_dependencies_type_check
		check (resource_type in (
			'openai_account', 'github_account', 'repository',
			'project_repository', 'chat_repository'
		))
);

create or replace function matter_codex_cluster_admin_profile_state(profile matter_codex_agent_profiles)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'name', profile.name,
		'role', profile.role,
		'description', profile.description,
		'enabled', profile.enabled,
		'openai_account_name', profile.openai_account_name,
		'github_account_name', profile.github_account_name,
		'kubernetes_access', profile.kubernetes_access,
		'sandbox_mode', profile.sandbox_mode,
		'config_overlay', profile.config_overlay
	)
$$;

create or replace function matter_codex_cluster_admin_role_state(role_row matter_codex_agent_roles)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'project_id', role_row.project_id,
		'name', role_row.name,
		'role_type', role_row.role_type,
		'description', role_row.description,
		'prompt_template', role_row.prompt_template,
		'prompt_mode', role_row.prompt_mode,
		'github_account_name', role_row.github_account_name,
		'project_github_account_name', coalesce(project.github_account_name, ''),
		'project_slug', project.slug,
		'project_mattermost_team_id', project.mattermost_team_id,
		'project_github_owner', project.github_owner,
		'project_github_owner_type', project.github_owner_type,
		'openai_account_name', role_row.openai_account_name,
		'kubernetes_access', role_row.kubernetes_access,
		'sandbox_mode', role_row.sandbox_mode,
		'config_overlay', role_row.config_overlay,
		'advanced_settings', role_row.advanced_settings,
		'enabled', role_row.enabled,
		'bot_identity', role_row.bot_identity
	)
	from matter_codex_projects project
	where project.id = role_row.project_id
$$;

create or replace function matter_codex_cluster_admin_bot_state(binding matter_codex_mattermost_bot_identities)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'project_id', binding.project_id,
		'role_id', binding.role_id,
		'username', binding.username,
		'display_name', binding.display_name,
		'mattermost_user_id', binding.mattermost_user_id,
		'token_secret_ref', binding.token_secret_ref,
		'status', binding.status
	)
$$;

create or replace function matter_codex_cluster_admin_runtime_variable_state(variable matter_codex_project_runtime_variables)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'project_id', variable.project_id,
		'name', variable.name,
		'slug', variable.slug,
		'description', variable.description,
		'secret_ref', variable.secret_ref,
		'secret_key', variable.secret_key,
		'sensitive', variable.sensitive,
		'enabled', variable.enabled
	)
$$;

create or replace function matter_codex_cluster_admin_session_state(session_row matter_codex_agent_sessions)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'session_key', session_row.session_key,
		'project_id', session_row.project_id,
		'chat_id', session_row.chat_id,
		'role_id', session_row.role_id,
		'session_scope', session_row.session_scope,
		'mattermost_channel_id', session_row.mattermost_channel_id,
		'mattermost_root_post_id', session_row.mattermost_root_post_id,
		'openai_account_name', session_row.openai_account_name,
		'kubernetes_namespace', session_row.kubernetes_namespace,
		'pod_name', session_row.pod_name,
		'pvc_name', session_row.pvc_name,
		'token_secret_ref', session_row.token_secret_ref,
		'capabilities', session_row.capabilities
	)
$$;

create or replace function matter_codex_cluster_admin_openai_account_state_values(
	account_name text,
	credential_id_value bigint,
	account_status text,
	model_policy_value text
)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'exists', true,
		'name', account_name,
		'credential_id', credential_id_value,
		'status', account_status,
		'model_policy', model_policy_value,
		'credential_name', coalesce(credential.name, ''),
		'credential_type', coalesce(credential.credential_type, ''),
		'credential_provider', coalesce(credential.provider, ''),
		'credential_secret_ref', coalesce(credential.secret_ref, ''),
		'credential_status', coalesce(credential.status, '')
	)
	from (select 1) anchor
	left join matter_codex_credentials credential on credential.id = credential_id_value
$$;

create or replace function matter_codex_cluster_admin_openai_account_state(account_name text)
returns jsonb
language sql
stable
as $$
	select coalesce(
		(
			select matter_codex_cluster_admin_openai_account_state_values(
				account.name, account.credential_id, account.status, account.model_policy
			)
			from matter_codex_openai_accounts account
			where account.name = account_name
		),
		jsonb_build_object('exists', false, 'name', account_name)
	)
$$;

create or replace function matter_codex_cluster_admin_github_account_state_values(
	account_name text,
	credential_id_value bigint,
	secret_ref_value text,
	username_value text,
	email_value text,
	account_status text
)
returns jsonb
language sql
stable
as $$
	select jsonb_build_object(
		'exists', true,
		'name', account_name,
		'credential_id', credential_id_value,
		'secret_ref', secret_ref_value,
		'username', username_value,
		'email', email_value,
		'status', account_status,
		'credential_name', coalesce(credential.name, ''),
		'credential_type', coalesce(credential.credential_type, ''),
		'credential_provider', coalesce(credential.provider, ''),
		'credential_secret_ref', coalesce(credential.secret_ref, ''),
		'credential_status', coalesce(credential.status, '')
	)
	from (select 1) anchor
	left join matter_codex_credentials credential on credential.id = credential_id_value
$$;

create or replace function matter_codex_cluster_admin_github_account_state(account_name text)
returns jsonb
language sql
stable
as $$
	select coalesce(
		(
			select matter_codex_cluster_admin_github_account_state_values(
				account.name, account.credential_id, account.secret_ref,
				account.username, account.email, account.status
			)
			from matter_codex_github_accounts account
			where account.name = account_name
		),
		jsonb_build_object('exists', false, 'name', account_name)
	)
$$;

create or replace function matter_codex_cluster_admin_repository_state_values(
	repository_id bigint,
	provider_value text,
	owner_value text,
	name_value text,
	default_branch_value text,
	status_value text,
	mattermost_channel_value text,
	github_account_name_value text
)
returns jsonb
language sql
immutable
as $$
	select jsonb_build_object(
		'exists', true,
		'id', repository_id,
		'provider', provider_value,
		'owner', owner_value,
		'name', name_value,
		'default_branch', default_branch_value,
		'status', status_value,
		'mattermost_channel', mattermost_channel_value,
		'github_account_name', github_account_name_value
	)
$$;

create or replace function matter_codex_cluster_admin_repository_state(repository_id bigint)
returns jsonb
language sql
stable
as $$
	select coalesce(
		(
			select matter_codex_cluster_admin_repository_state_values(
				repository.id, repository.provider, repository.owner, repository.name,
				repository.default_branch, repository.status, repository.mattermost_channel,
				repository.github_account_name
			)
			from matter_codex_repositories repository
			where repository.id = repository_id
		),
		jsonb_build_object('exists', false, 'id', repository_id)
	)
$$;

create or replace function matter_codex_cluster_admin_project_repository_state_values(
	binding_id bigint,
	project_id_value bigint,
	repository_id_value bigint,
	is_default_value boolean,
	metadata_value jsonb
)
returns jsonb
language sql
immutable
as $$
	select jsonb_build_object(
		'exists', true,
		'id', binding_id,
		'project_id', project_id_value,
		'repository_id', repository_id_value,
		'is_default', is_default_value,
		'metadata', metadata_value
	)
$$;

create or replace function matter_codex_cluster_admin_project_repository_state(binding_id bigint)
returns jsonb
language sql
stable
as $$
	select coalesce(
		(
			select matter_codex_cluster_admin_project_repository_state_values(
				binding.id, binding.project_id, binding.repository_id,
				binding.is_default, binding.metadata
			)
			from matter_codex_project_repositories binding
			where binding.id = binding_id
		),
		jsonb_build_object('exists', false, 'id', binding_id)
	)
$$;

create or replace function matter_codex_cluster_admin_chat_repository_state_values(
	binding_id bigint,
	chat_id_value bigint,
	repository_id_value bigint
)
returns jsonb
language sql
immutable
as $$
	select jsonb_build_object(
		'exists', true,
		'id', binding_id,
		'chat_id', chat_id_value,
		'repository_id', repository_id_value
	)
$$;

create or replace function matter_codex_cluster_admin_chat_repository_state(binding_id bigint)
returns jsonb
language sql
stable
as $$
	select coalesce(
		(
			select matter_codex_cluster_admin_chat_repository_state_values(
				binding.id, binding.chat_id, binding.repository_id
			)
			from matter_codex_chat_repositories binding
			where binding.id = binding_id
		),
		jsonb_build_object('exists', false, 'id', binding_id)
	)
$$;

create or replace function matter_codex_cluster_admin_dependency_state(
	resource_type_value text,
	resource_key_value text
)
returns jsonb
language sql
stable
as $$
	select case resource_type_value
		when 'openai_account' then matter_codex_cluster_admin_openai_account_state(resource_key_value)
		when 'github_account' then matter_codex_cluster_admin_github_account_state(resource_key_value)
		when 'repository' then matter_codex_cluster_admin_repository_state(resource_key_value::bigint)
		when 'project_repository' then matter_codex_cluster_admin_project_repository_state(resource_key_value::bigint)
		when 'chat_repository' then matter_codex_cluster_admin_chat_repository_state(resource_key_value::bigint)
		else jsonb_build_object('exists', false, 'unsupported', resource_type_value, 'key', resource_key_value)
	end
$$;

insert into matter_codex_cluster_admin_subjects(
	subject_type, subject_key, project_id, profile_name, privilege_state
)
select 'agent_profile', profile.name, 0, profile.name,
	matter_codex_cluster_admin_profile_state(profile)
from matter_codex_agent_profiles profile
where lower(trim(profile.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_subjects(
	subject_type, subject_key, project_id, profile_name, privilege_state
)
select 'agent_role', role.id::text, role.project_id, role.name,
	matter_codex_cluster_admin_role_state(role)
from matter_codex_agent_roles role
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from matter_codex_agent_roles role
		left join matter_codex_mattermost_bot_identities binding
			on binding.role_id = role.id
		where lower(trim(role.kubernetes_access)) = 'cluster-admin'
			and (
				binding.id is null
				or trim(binding.mattermost_user_id) = ''
				or trim(binding.token_secret_ref) = ''
			)
	) then
		raise exception 'cluster-admin freeze requires an existing exact bot binding'
			using errcode = 'check_violation';
	end if;
end
$$;
-- +goose StatementEnd

insert into matter_codex_cluster_admin_bot_bindings(role_id, project_id, privilege_state)
select role.id, role.project_id, matter_codex_cluster_admin_bot_state(binding)
from matter_codex_agent_roles role
join matter_codex_mattermost_bot_identities binding on binding.role_id = role.id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_runtime_variable_bindings(role_id, variable_id, privilege_state)
select role.id, variable.id, matter_codex_cluster_admin_runtime_variable_state(variable)
from matter_codex_agent_roles role
join matter_codex_agent_role_runtime_variables role_variable on role_variable.role_id = role.id
join matter_codex_project_runtime_variables variable on variable.id = role_variable.variable_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_dependencies(
	role_id, resource_type, resource_key, privilege_state
)
select role.id, 'openai_account', trim(role.openai_account_name),
	matter_codex_cluster_admin_openai_account_state(trim(role.openai_account_name))
from matter_codex_agent_roles role
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
	and trim(role.openai_account_name) <> ''
on conflict do nothing;

with cluster_admin_github_accounts as (
	select role.id as role_id, nullif(trim(role.github_account_name), '') as account_name
	from matter_codex_agent_roles role
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
	union
	select role.id, nullif(trim(project.github_account_name), '')
	from matter_codex_agent_roles role
	join matter_codex_projects project on project.id = role.project_id
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
	union
	select role.id, nullif(trim(repository.github_account_name), '')
	from matter_codex_agent_roles role
	join matter_codex_project_repositories project_repository on project_repository.project_id = role.project_id
	join matter_codex_repositories repository on repository.id = project_repository.repository_id
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
	union
	select role.id, nullif(trim(repository.github_account_name), '')
	from matter_codex_agent_roles role
	join matter_codex_chat_participants participant on participant.role_id = role.id and participant.enabled
	join matter_codex_chat_repositories chat_repository on chat_repository.chat_id = participant.chat_id
	join matter_codex_repositories repository on repository.id = chat_repository.repository_id
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
)
insert into matter_codex_cluster_admin_dependencies(
	role_id, resource_type, resource_key, privilege_state
)
select distinct role_id, 'github_account', account_name,
	matter_codex_cluster_admin_github_account_state(account_name)
from cluster_admin_github_accounts
where account_name is not null
on conflict do nothing;

with cluster_admin_repositories as (
	select role.id as role_id, repository.id as repository_id
	from matter_codex_agent_roles role
	join matter_codex_project_repositories project_repository on project_repository.project_id = role.project_id
	join matter_codex_repositories repository on repository.id = project_repository.repository_id
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
	union
	select role.id, repository.id
	from matter_codex_agent_roles role
	join matter_codex_chat_participants participant on participant.role_id = role.id and participant.enabled
	join matter_codex_chat_repositories chat_repository on chat_repository.chat_id = participant.chat_id
	join matter_codex_repositories repository on repository.id = chat_repository.repository_id
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
)
insert into matter_codex_cluster_admin_dependencies(
	role_id, resource_type, resource_key, privilege_state
)
select role_id, 'repository', repository_id::text,
	matter_codex_cluster_admin_repository_state(repository_id)
from cluster_admin_repositories
on conflict do nothing;

insert into matter_codex_cluster_admin_dependencies(
	role_id, resource_type, resource_key, privilege_state
)
select role.id, 'project_repository', binding.id::text,
	matter_codex_cluster_admin_project_repository_state(binding.id)
from matter_codex_agent_roles role
join matter_codex_project_repositories binding on binding.project_id = role.project_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_dependencies(
	role_id, resource_type, resource_key, privilege_state
)
select role.id, 'chat_repository', binding.id::text,
	matter_codex_cluster_admin_chat_repository_state(binding.id)
from matter_codex_agent_roles role
join matter_codex_chat_participants participant on participant.role_id = role.id and participant.enabled
join matter_codex_chat_repositories binding on binding.chat_id = participant.chat_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_bindings(role_id, project_id, chat_id, mattermost_channel_id)
select participant.role_id, role.project_id, participant.chat_id, chat.mattermost_channel_id
from matter_codex_chat_participants participant
join matter_codex_agent_roles role on role.id = participant.role_id
join matter_codex_chats chat on chat.id = participant.chat_id and chat.project_id = role.project_id
where participant.enabled
	and lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_session_bindings(
	role_id, project_id, chat_id, session_key, mattermost_channel_id, privilege_state
)
select session_row.role_id, session_row.project_id, session_row.chat_id,
	session_row.session_key, session_row.mattermost_channel_id,
	matter_codex_cluster_admin_session_state(session_row)
from matter_codex_agent_sessions session_row
join matter_codex_agent_roles role
	on role.id = session_row.role_id
	and role.project_id = session_row.project_id
join matter_codex_cluster_admin_bindings binding
	on binding.role_id = session_row.role_id
	and binding.project_id = session_row.project_id
	and binding.chat_id = session_row.chat_id
	and binding.mattermost_channel_id = session_row.mattermost_channel_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
select 'session_binding', session_row.role_id::text || ':' || session_row.session_key,
	'session ' || session_row.status
from matter_codex_agent_sessions session_row
join matter_codex_cluster_admin_session_bindings frozen
	on frozen.role_id = session_row.role_id
	and frozen.session_key = session_row.session_key
where session_row.status in ('blocked', 'closed')
on conflict do nothing;

create or replace function matter_codex_cluster_admin_role_exact(candidate_role_id bigint)
returns boolean
language sql
stable
as $$
	select exists(
		select 1
		from matter_codex_agent_roles role
		join matter_codex_cluster_admin_subjects subject
			on subject.subject_type = 'agent_role'
			and subject.subject_key = role.id::text
			and subject.project_id = role.project_id
		join matter_codex_mattermost_bot_identities bot on bot.role_id = role.id
		join matter_codex_cluster_admin_bot_bindings frozen_bot
			on frozen_bot.role_id = role.id
			and frozen_bot.project_id = role.project_id
		where role.id = candidate_role_id
			and role.enabled
			and lower(trim(role.kubernetes_access)) = 'cluster-admin'
			and subject.privilege_state = matter_codex_cluster_admin_role_state(role)
			and frozen_bot.privilege_state = matter_codex_cluster_admin_bot_state(bot)
			and not exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where (revocation.resource_type, revocation.resource_key) in (
					('agent_role', role.id::text),
					('bot_binding', role.id::text)
				)
			)
			and not exists (
				select 1
				from matter_codex_cluster_admin_runtime_variable_bindings frozen_variable
				left join matter_codex_agent_role_runtime_variables role_variable
					on role_variable.role_id = frozen_variable.role_id
					and role_variable.variable_id = frozen_variable.variable_id
				left join matter_codex_project_runtime_variables variable
					on variable.id = role_variable.variable_id
				where frozen_variable.role_id = role.id
					and (
						variable.id is null
						or frozen_variable.privilege_state <> matter_codex_cluster_admin_runtime_variable_state(variable)
						or exists (
							select 1 from matter_codex_cluster_admin_revocations revocation
							where revocation.resource_type = 'runtime_variable_binding'
								and revocation.resource_key = role.id::text || ':' || frozen_variable.variable_id::text
						)
					)
			)
			and not exists (
				select 1
				from matter_codex_agent_role_runtime_variables role_variable
				where role_variable.role_id = role.id
					and not exists (
						select 1
						from matter_codex_cluster_admin_runtime_variable_bindings frozen_variable
						where frozen_variable.role_id = role_variable.role_id
							and frozen_variable.variable_id = role_variable.variable_id
					)
			)
			and not exists (
				select 1
				from matter_codex_cluster_admin_dependencies dependency
				where dependency.role_id = role.id
					and (
						dependency.privilege_state <> matter_codex_cluster_admin_dependency_state(
							dependency.resource_type, dependency.resource_key
						)
						or exists (
							select 1 from matter_codex_cluster_admin_revocations revocation
							where revocation.resource_type = 'dependency'
								and revocation.resource_key = role.id::text || ':' || dependency.resource_type || ':' || dependency.resource_key
						)
					)
			)
			and not exists (
				select 1
				from matter_codex_project_repositories binding
				where binding.project_id = role.project_id
					and not exists (
						select 1 from matter_codex_cluster_admin_dependencies dependency
						where dependency.role_id = role.id
							and dependency.resource_type = 'project_repository'
							and dependency.resource_key = binding.id::text
					)
			)
			and not exists (
				select 1
				from matter_codex_cluster_admin_bindings frozen_chat
				join matter_codex_chat_repositories binding on binding.chat_id = frozen_chat.chat_id
				where frozen_chat.role_id = role.id
					and not exists (
						select 1 from matter_codex_cluster_admin_dependencies dependency
						where dependency.role_id = role.id
							and dependency.resource_type = 'chat_repository'
							and dependency.resource_key = binding.id::text
					)
			)
	)
$$;

create or replace function matter_codex_cluster_admin_binding_exact(candidate_role_id bigint, candidate_chat_id bigint)
returns boolean
language sql
stable
as $$
	select matter_codex_cluster_admin_role_exact(candidate_role_id)
		and exists(
			select 1
			from matter_codex_cluster_admin_bindings binding
			join matter_codex_chats chat
				on chat.id = binding.chat_id
				and chat.project_id = binding.project_id
				and chat.mattermost_channel_id = binding.mattermost_channel_id
			join matter_codex_chat_participants participant
				on participant.role_id = binding.role_id
				and participant.chat_id = binding.chat_id
				and participant.enabled
			where binding.role_id = candidate_role_id
				and binding.chat_id = candidate_chat_id
				and not exists (
					select 1 from matter_codex_cluster_admin_revocations revocation
					where revocation.resource_type = 'chat_binding'
						and revocation.resource_key = binding.role_id::text || ':' || binding.chat_id::text
				)
		)
$$;

-- +goose StatementBegin
create or replace function matter_codex_cluster_admin_record_denied(
	resource_type_value text,
	resource_key_value text,
	operation_value text
)
returns void
language plpgsql
as $$
begin
	insert into matter_codex_audit_events(
		event_type, actor_user, resource_type, resource_name, summary
	) values (
		'cluster_admin.freeze.denied', 'database', resource_type_value,
		resource_key_value, operation_value || ': denied'
	);
end
$$;

create or replace function matter_codex_guard_cluster_admin_profile()
returns trigger
language plpgsql
as $$
declare
	frozen_state jsonb;
	revocation_key text;
begin
	if tg_op = 'INSERT' then
		revocation_key := new.name;
	else
		revocation_key := old.name;
	end if;
	select subject.privilege_state into frozen_state
	from matter_codex_cluster_admin_subjects subject
	where subject.subject_type = 'agent_profile'
		and subject.subject_key = revocation_key
		and subject.project_id = 0;
	if tg_op = 'DELETE' then
		if frozen_state is not null then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('agent_profile', revocation_key, 'deleted') on conflict do nothing;
		end if;
		return old;
	end if;
	if tg_op = 'INSERT' then
		if lower(trim(new.kubernetes_access)) <> 'cluster-admin' then
			return new;
		end if;
		select subject.privilege_state into frozen_state
		from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_profile'
			and subject.subject_key = new.name
			and subject.project_id = 0;
	end if;
	if frozen_state is null then
		if lower(trim(new.kubernetes_access)) = 'cluster-admin' then
			perform matter_codex_cluster_admin_record_denied('agent_profile', new.name, 'profile.insert');
			return null;
		end if;
		return new;
	end if;
	if lower(trim(new.kubernetes_access)) <> 'cluster-admin' then
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('agent_profile', revocation_key, 'kubernetes_access downgraded') on conflict do nothing;
		return new;
	end if;
	if frozen_state = matter_codex_cluster_admin_profile_state(new)
		and not exists (
			select 1 from matter_codex_cluster_admin_revocations
			where resource_type = 'agent_profile' and resource_key = revocation_key
		)
	then
		return new;
	end if;
	if tg_op = 'UPDATE'
		and old.enabled
		and not new.enabled
		and (frozen_state - 'enabled') = (matter_codex_cluster_admin_profile_state(new) - 'enabled')
	then
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('agent_profile', revocation_key, 'disabled') on conflict do nothing;
		return new;
	end if;
	perform matter_codex_cluster_admin_record_denied('agent_profile', revocation_key, 'profile.mutation');
	return null;
end
$$;

create or replace function matter_codex_guard_cluster_admin_role()
returns trigger
language plpgsql
as $$
declare
	frozen_state jsonb;
	existing_role_id bigint;
	revocation_key text;
begin
	if tg_op = 'DELETE' then
		revocation_key := old.id::text;
		select subject.privilege_state into frozen_state
		from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_role'
			and subject.subject_key = revocation_key
			and subject.project_id = old.project_id;
		if frozen_state is not null then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('agent_role', revocation_key, 'deleted') on conflict do nothing;
		end if;
		return old;
	end if;
	if tg_op = 'INSERT' then
		if lower(trim(new.kubernetes_access)) <> 'cluster-admin' then
			return new;
		end if;
		select role.id into existing_role_id
		from matter_codex_agent_roles role
		where role.project_id = new.project_id and role.name = new.name;
		if existing_role_id is null then
			perform matter_codex_cluster_admin_record_denied('agent_role', new.name, 'role.insert');
			return null;
		end if;
		revocation_key := existing_role_id::text;
		select subject.privilege_state into frozen_state
		from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_role'
			and subject.subject_key = revocation_key
			and subject.project_id = new.project_id;
	else
		revocation_key := old.id::text;
		select subject.privilege_state into frozen_state
		from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_role'
			and subject.subject_key = revocation_key
			and subject.project_id = old.project_id;
	end if;
	if frozen_state is null then
		if lower(trim(new.kubernetes_access)) = 'cluster-admin' then
			perform matter_codex_cluster_admin_record_denied('agent_role', new.name, 'role.mutation');
			return null;
		end if;
		return new;
	end if;
	if lower(trim(new.kubernetes_access)) <> 'cluster-admin' then
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('agent_role', revocation_key, 'kubernetes_access downgraded') on conflict do nothing;
		return new;
	end if;
	if frozen_state = matter_codex_cluster_admin_role_state(new)
		and not exists (
			select 1 from matter_codex_cluster_admin_revocations
			where resource_type = 'agent_role' and resource_key = revocation_key
		)
	then
		return new;
	end if;
	if tg_op = 'UPDATE'
		and old.enabled
		and not new.enabled
		and (frozen_state - 'enabled') = (matter_codex_cluster_admin_role_state(new) - 'enabled')
	then
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('agent_role', revocation_key, 'disabled') on conflict do nothing;
		return new;
	end if;
	perform matter_codex_cluster_admin_record_denied('agent_role', revocation_key, 'role.mutation');
	return null;
end
$$;

create or replace function matter_codex_guard_cluster_admin_project()
returns trigger
language plpgsql
as $$
begin
	if (
		old.github_account_name is distinct from new.github_account_name
		or old.slug is distinct from new.slug
		or old.mattermost_team_id is distinct from new.mattermost_team_id
		or old.github_owner is distinct from new.github_owner
		or old.github_owner_type is distinct from new.github_owner_type
	)
		and exists (
			select 1 from matter_codex_agent_roles role
			where role.project_id = old.id
				and matter_codex_cluster_admin_role_exact(role.id)
		)
	then
		perform matter_codex_cluster_admin_record_denied('project', old.id::text, 'project.binding.mutation');
		return null;
	end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_bot_binding()
returns trigger
language plpgsql
as $$
declare
	frozen_state jsonb;
	revocation_key text;
begin
	if tg_op = 'INSERT' then
		revocation_key := new.role_id::text;
	else
		revocation_key := old.role_id::text;
	end if;
	select binding.privilege_state into frozen_state
	from matter_codex_cluster_admin_bot_bindings binding
	where binding.role_id::text = revocation_key;
	if tg_op = 'DELETE' then
		if frozen_state is not null then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('bot_binding', revocation_key, 'deleted') on conflict do nothing;
		end if;
		return old;
	end if;
	if (
		frozen_state is not null
		or exists (
			select 1 from matter_codex_cluster_admin_subjects subject
			where subject.subject_type = 'agent_role'
				and subject.subject_key = new.role_id::text
				and subject.project_id = new.project_id
		)
	) and (
		frozen_state is null
		or frozen_state <> matter_codex_cluster_admin_bot_state(new)
		or exists (
			select 1 from matter_codex_cluster_admin_revocations
			where resource_type in ('agent_role', 'bot_binding')
				and resource_key = revocation_key
		)
	) then
		perform matter_codex_cluster_admin_record_denied('bot_binding', revocation_key, 'bot_binding.mutation');
		return null;
	end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_runtime_variable_binding()
returns trigger
language plpgsql
as $$
declare
	key_value text;
	frozen_state jsonb;
	variable_state jsonb;
begin
	if tg_op = 'INSERT' then
		key_value := new.role_id::text || ':' || new.variable_id::text;
	else
		key_value := old.role_id::text || ':' || old.variable_id::text;
	end if;
	select binding.privilege_state into frozen_state
	from matter_codex_cluster_admin_runtime_variable_bindings binding
	where binding.role_id::text || ':' || binding.variable_id::text = key_value;
	if tg_op = 'UPDATE' and frozen_state is not null
		and (old.role_id is distinct from new.role_id or old.variable_id is distinct from new.variable_id)
	then
		perform matter_codex_cluster_admin_record_denied('runtime_variable_binding', key_value, 'runtime_variable_binding.remap');
		return null;
	end if;
	if tg_op = 'DELETE' then
		if frozen_state is not null then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('runtime_variable_binding', key_value, 'deleted') on conflict do nothing;
		end if;
		return old;
	end if;
	select matter_codex_cluster_admin_runtime_variable_state(variable) into variable_state
	from matter_codex_project_runtime_variables variable where variable.id = new.variable_id;
	if (
		frozen_state is not null
		or exists (
			select 1 from matter_codex_cluster_admin_subjects subject
			where subject.subject_type = 'agent_role' and subject.subject_key = new.role_id::text
		)
	) and (
		frozen_state is null
		or frozen_state <> variable_state
		or exists (
			select 1 from matter_codex_cluster_admin_revocations
			where resource_type = 'runtime_variable_binding' and resource_key = key_value
		)
	) then
		perform matter_codex_cluster_admin_record_denied('runtime_variable_binding', key_value, 'runtime_variable_binding.mutation');
		return null;
	end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_runtime_variable()
returns trigger
language plpgsql
as $$
declare
	frozen_binding record;
begin
	for frozen_binding in
		select binding.role_id, binding.variable_id, binding.privilege_state
		from matter_codex_cluster_admin_runtime_variable_bindings binding
		where binding.variable_id = old.id
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('runtime_variable_binding', frozen_binding.role_id::text || ':' || old.id::text, 'variable deleted')
			on conflict do nothing;
			continue;
		end if;
		if frozen_binding.privilege_state = matter_codex_cluster_admin_runtime_variable_state(new) then
			if exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where revocation.resource_type = 'runtime_variable_binding'
					and revocation.resource_key = frozen_binding.role_id::text || ':' || old.id::text
			) then
				perform matter_codex_cluster_admin_record_denied(
					'runtime_variable_binding', frozen_binding.role_id::text || ':' || old.id::text,
					'runtime_variable.reenable'
				);
				return null;
			end if;
			continue;
		end if;
		if old.enabled and not new.enabled
			and (frozen_binding.privilege_state - 'enabled') = (matter_codex_cluster_admin_runtime_variable_state(new) - 'enabled')
		then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('runtime_variable_binding', frozen_binding.role_id::text || ':' || old.id::text, 'variable disabled')
			on conflict do nothing;
			continue;
		end if;
		perform matter_codex_cluster_admin_record_denied(
			'runtime_variable_binding', frozen_binding.role_id::text || ':' || old.id::text,
			'runtime_variable.mutation'
		);
		return null;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_openai_account()
returns trigger
language plpgsql
as $$
declare
	dependency record;
	key_value text;
	proposed_state jsonb;
begin
	key_value := case when tg_op = 'INSERT' then new.name else old.name end;
	for dependency in
		select role_id, privilege_state
		from matter_codex_cluster_admin_dependencies
		where resource_type = 'openai_account' and resource_key = key_value
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('dependency', dependency.role_id::text || ':openai_account:' || key_value, 'account deleted')
			on conflict do nothing;
			continue;
		end if;
		proposed_state := matter_codex_cluster_admin_openai_account_state_values(
			new.name, new.credential_id, new.status, new.model_policy
		);
		if dependency.privilege_state <> proposed_state
			or exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where revocation.resource_type = 'dependency'
					and revocation.resource_key = dependency.role_id::text || ':openai_account:' || key_value
			)
		then
			perform matter_codex_cluster_admin_record_denied(
				'openai_account', dependency.role_id::text || ':' || key_value, 'openai_account.mutation'
			);
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_github_account()
returns trigger
language plpgsql
as $$
declare
	dependency record;
	key_value text;
	proposed_state jsonb;
begin
	key_value := case when tg_op = 'INSERT' then new.name else old.name end;
	for dependency in
		select role_id, privilege_state
		from matter_codex_cluster_admin_dependencies
		where resource_type = 'github_account' and resource_key = key_value
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('dependency', dependency.role_id::text || ':github_account:' || key_value, 'account deleted')
			on conflict do nothing;
			continue;
		end if;
		proposed_state := matter_codex_cluster_admin_github_account_state_values(
			new.name, new.credential_id, new.secret_ref, new.username, new.email, new.status
		);
		if dependency.privilege_state <> proposed_state
			or exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where revocation.resource_type = 'dependency'
					and revocation.resource_key = dependency.role_id::text || ':github_account:' || key_value
			)
		then
			perform matter_codex_cluster_admin_record_denied(
				'github_account', dependency.role_id::text || ':' || key_value, 'github_account.mutation'
			);
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_credential()
returns trigger
language plpgsql
as $$
declare
	dependency record;
begin
	for dependency in
		select distinct frozen.role_id, frozen.resource_type, frozen.resource_key
		from matter_codex_cluster_admin_dependencies frozen
		join matter_codex_openai_accounts openai_account
			on frozen.resource_type = 'openai_account'
			and frozen.resource_key = openai_account.name
			and openai_account.credential_id = old.id
		union
		select distinct frozen.role_id, frozen.resource_type, frozen.resource_key
		from matter_codex_cluster_admin_dependencies frozen
		join matter_codex_github_accounts github_account
			on frozen.resource_type = 'github_account'
			and frozen.resource_key = github_account.name
			and github_account.credential_id = old.id
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values (
				'dependency', dependency.role_id::text || ':' || dependency.resource_type || ':' || dependency.resource_key,
				'credential deleted'
			) on conflict do nothing;
			continue;
		end if;
		if old is distinct from new then
			perform matter_codex_cluster_admin_record_denied(
				'credential', dependency.role_id::text || ':' || dependency.resource_type || ':' || dependency.resource_key,
				'credential.mutation'
			);
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_repository()
returns trigger
language plpgsql
as $$
declare
	dependency record;
	key_value text;
	proposed_state jsonb;
begin
	key_value := case when tg_op = 'INSERT' then new.id::text else old.id::text end;
	for dependency in
		select role_id, privilege_state
		from matter_codex_cluster_admin_dependencies
		where resource_type = 'repository' and resource_key = key_value
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('dependency', dependency.role_id::text || ':repository:' || key_value, 'repository deleted')
			on conflict do nothing;
			continue;
		end if;
		proposed_state := matter_codex_cluster_admin_repository_state_values(
			new.id, new.provider, new.owner, new.name, new.default_branch,
			new.status, new.mattermost_channel, new.github_account_name
		);
		if dependency.privilege_state <> proposed_state
			or exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where revocation.resource_type = 'dependency'
					and revocation.resource_key = dependency.role_id::text || ':repository:' || key_value
			)
		then
			perform matter_codex_cluster_admin_record_denied(
				'repository', dependency.role_id::text || ':' || key_value, 'repository.mutation'
			);
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_project_repository()
returns trigger
language plpgsql
as $$
declare
	dependency record;
	key_value text;
	proposed_state jsonb;
	project_id_value bigint;
begin
	key_value := case when tg_op = 'INSERT' then new.id::text else old.id::text end;
	project_id_value := case when tg_op = 'INSERT' then new.project_id else old.project_id end;
	for dependency in
		select frozen.role_id, frozen.privilege_state
		from matter_codex_cluster_admin_dependencies frozen
		where frozen.resource_type = 'project_repository'
			and frozen.resource_key = key_value
		union
		select subject.subject_key::bigint, null::jsonb
		from matter_codex_cluster_admin_subjects subject
		where tg_op = 'INSERT'
			and subject.subject_type = 'agent_role'
			and subject.project_id = project_id_value
			and not exists (
				select 1 from matter_codex_cluster_admin_dependencies frozen
				where frozen.role_id = subject.subject_key::bigint
					and frozen.resource_type = 'project_repository'
					and frozen.resource_key = key_value
			)
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('dependency', dependency.role_id::text || ':project_repository:' || key_value, 'binding deleted')
			on conflict do nothing;
			continue;
		end if;
		proposed_state := matter_codex_cluster_admin_project_repository_state_values(
			new.id, new.project_id, new.repository_id, new.is_default, new.metadata
		);
		if dependency.privilege_state is null
			or dependency.privilege_state <> proposed_state
			or exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where revocation.resource_type = 'dependency'
					and revocation.resource_key = dependency.role_id::text || ':project_repository:' || key_value
			)
		then
			perform matter_codex_cluster_admin_record_denied(
				'project_repository', dependency.role_id::text || ':' || key_value, 'project_repository.mutation'
			);
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_chat_repository()
returns trigger
language plpgsql
as $$
declare
	dependency record;
	key_value text;
	proposed_state jsonb;
	chat_id_value bigint;
begin
	key_value := case when tg_op = 'INSERT' then new.id::text else old.id::text end;
	chat_id_value := case when tg_op = 'INSERT' then new.chat_id else old.chat_id end;
	for dependency in
		select frozen.role_id, frozen.privilege_state
		from matter_codex_cluster_admin_dependencies frozen
		where frozen.resource_type = 'chat_repository'
			and frozen.resource_key = key_value
		union
		select binding.role_id, null::jsonb
		from matter_codex_cluster_admin_bindings binding
		where tg_op = 'INSERT'
			and binding.chat_id = chat_id_value
			and not exists (
				select 1 from matter_codex_cluster_admin_dependencies frozen
				where frozen.role_id = binding.role_id
					and frozen.resource_type = 'chat_repository'
					and frozen.resource_key = key_value
			)
	loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('dependency', dependency.role_id::text || ':chat_repository:' || key_value, 'binding deleted')
			on conflict do nothing;
			continue;
		end if;
		proposed_state := matter_codex_cluster_admin_chat_repository_state_values(
			new.id, new.chat_id, new.repository_id
		);
		if dependency.privilege_state is null
			or dependency.privilege_state <> proposed_state
			or exists (
				select 1 from matter_codex_cluster_admin_revocations revocation
				where revocation.resource_type = 'dependency'
					and revocation.resource_key = dependency.role_id::text || ':chat_repository:' || key_value
			)
		then
			perform matter_codex_cluster_admin_record_denied(
				'chat_repository', dependency.role_id::text || ':' || key_value, 'chat_repository.mutation'
			);
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_chat()
returns trigger
language plpgsql
as $$
declare
	binding record;
begin
	for binding in select role_id, chat_id from matter_codex_cluster_admin_bindings where chat_id = old.id loop
		if tg_op = 'DELETE' then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('chat_binding', binding.role_id::text || ':' || binding.chat_id::text, 'chat deleted')
			on conflict do nothing;
			continue;
		end if;
		if old.id is distinct from new.id
			or old.project_id is distinct from new.project_id
			or old.mattermost_channel_id is distinct from new.mattermost_channel_id
		then
			perform matter_codex_cluster_admin_record_denied('chat_binding', binding.role_id::text || ':' || binding.chat_id::text, 'chat.channel.mutation');
			return null;
		end if;
	end loop;
	if tg_op = 'DELETE' then return old; end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_participant()
returns trigger
language plpgsql
as $$
declare
	key_value text;
begin
	if tg_op = 'INSERT' then
		key_value := new.role_id::text || ':' || new.chat_id::text;
	else
		key_value := old.role_id::text || ':' || old.chat_id::text;
	end if;
	if tg_op = 'UPDATE'
		and (old.role_id is distinct from new.role_id or old.chat_id is distinct from new.chat_id)
		and exists (
			select 1 from matter_codex_cluster_admin_bindings binding
			where binding.role_id = old.role_id and binding.chat_id = old.chat_id
		)
	then
		perform matter_codex_cluster_admin_record_denied('chat_binding', key_value, 'participant.remap');
		return null;
	end if;
	if tg_op = 'DELETE' or (tg_op = 'UPDATE' and old.enabled and not new.enabled) then
		if exists (
			select 1 from matter_codex_cluster_admin_bindings binding
			where binding.role_id = old.role_id and binding.chat_id = old.chat_id
		) then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('chat_binding', key_value, case when tg_op = 'DELETE' then 'participant deleted' else 'participant disabled' end)
			on conflict do nothing;
		end if;
		if tg_op = 'DELETE' then return old; end if;
		return new;
	end if;
	if new.enabled and exists (
		select 1 from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_role' and subject.subject_key = new.role_id::text
	) and not matter_codex_cluster_admin_binding_exact(new.role_id, new.chat_id) then
		perform matter_codex_cluster_admin_record_denied('chat_binding', key_value, 'participant.mutation');
		return null;
	end if;
	return new;
end
$$;

create or replace function matter_codex_guard_cluster_admin_session()
returns trigger
language plpgsql
as $$
declare
	frozen_state jsonb;
	key_value text;
begin
	if tg_op = 'INSERT' then
		key_value := new.role_id::text || ':' || new.session_key;
	else
		key_value := old.role_id::text || ':' || old.session_key;
	end if;
	select binding.privilege_state into frozen_state
	from matter_codex_cluster_admin_session_bindings binding
	where binding.role_id::text || ':' || binding.session_key = key_value;
	if tg_op = 'DELETE' then
		if frozen_state is not null then
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('session_binding', key_value, 'session deleted') on conflict do nothing;
		end if;
		return old;
	end if;
	if tg_op = 'UPDATE'
		and old.status in ('blocked', 'closed')
		and new.status is distinct from old.status
	then
		perform matter_codex_cluster_admin_record_denied('session_binding', key_value, 'session.reenable');
		return null;
	end if;
	if tg_op = 'UPDATE'
		and old.status not in ('blocked', 'closed')
		and new.status in ('blocked', 'closed')
		and frozen_state is not null
	then
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('session_binding', key_value, 'session ' || new.status) on conflict do nothing;
		return new;
	end if;
	if (
		frozen_state is not null
		or exists (
			select 1 from matter_codex_cluster_admin_subjects subject
			where subject.subject_type = 'agent_role' and subject.subject_key = new.role_id::text
		)
	) and (
		frozen_state is null
		or frozen_state <> matter_codex_cluster_admin_session_state(new)
		or not matter_codex_cluster_admin_binding_exact(new.role_id, new.chat_id)
		or exists (
			select 1 from matter_codex_cluster_admin_revocations
			where resource_type = 'session_binding' and resource_key = key_value
		)
	) then
		perform matter_codex_cluster_admin_record_denied('session_binding', key_value, 'session.mutation');
		return null;
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_cluster_admin_profile_guard
before insert or update or delete on matter_codex_agent_profiles
for each row execute function matter_codex_guard_cluster_admin_profile();

create trigger matter_codex_cluster_admin_role_guard
before insert or update or delete on matter_codex_agent_roles
for each row execute function matter_codex_guard_cluster_admin_role();

create trigger matter_codex_cluster_admin_project_guard
before update on matter_codex_projects
for each row execute function matter_codex_guard_cluster_admin_project();

create trigger matter_codex_cluster_admin_bot_binding_guard
before insert or update or delete on matter_codex_mattermost_bot_identities
for each row execute function matter_codex_guard_cluster_admin_bot_binding();

create trigger matter_codex_cluster_admin_runtime_variable_binding_guard
before insert or update or delete on matter_codex_agent_role_runtime_variables
for each row execute function matter_codex_guard_cluster_admin_runtime_variable_binding();

create trigger matter_codex_cluster_admin_runtime_variable_guard
before update or delete on matter_codex_project_runtime_variables
for each row execute function matter_codex_guard_cluster_admin_runtime_variable();

create trigger matter_codex_cluster_admin_openai_account_guard
before insert or update or delete on matter_codex_openai_accounts
for each row execute function matter_codex_guard_cluster_admin_openai_account();

create trigger matter_codex_cluster_admin_github_account_guard
before insert or update or delete on matter_codex_github_accounts
for each row execute function matter_codex_guard_cluster_admin_github_account();

create trigger matter_codex_cluster_admin_credential_guard
before update or delete on matter_codex_credentials
for each row execute function matter_codex_guard_cluster_admin_credential();

create trigger matter_codex_cluster_admin_repository_guard
before insert or update or delete on matter_codex_repositories
for each row execute function matter_codex_guard_cluster_admin_repository();

create trigger matter_codex_cluster_admin_project_repository_guard
before insert or update or delete on matter_codex_project_repositories
for each row execute function matter_codex_guard_cluster_admin_project_repository();

create trigger matter_codex_cluster_admin_chat_repository_guard
before insert or update or delete on matter_codex_chat_repositories
for each row execute function matter_codex_guard_cluster_admin_chat_repository();

create trigger matter_codex_cluster_admin_chat_guard
before update or delete on matter_codex_chats
for each row execute function matter_codex_guard_cluster_admin_chat();

create trigger matter_codex_cluster_admin_participant_guard
before insert or update or delete on matter_codex_chat_participants
for each row execute function matter_codex_guard_cluster_admin_participant();

create trigger matter_codex_cluster_admin_session_guard
before insert or update or delete on matter_codex_agent_sessions
for each row execute function matter_codex_guard_cluster_admin_session();

create or replace function matter_codex_require_cluster_admin_freeze_writer()
returns boolean
language sql
stable
as $$
	select current_setting('matter_codex.cluster_admin_freeze_writer', true) = 'v23'
$$;

alter table matter_codex_agent_profiles enable row level security;
alter table matter_codex_agent_profiles force row level security;
create policy matter_codex_agent_profiles_freeze_writer on matter_codex_agent_profiles
	using (matter_codex_require_cluster_admin_freeze_writer())
	with check (matter_codex_require_cluster_admin_freeze_writer());

alter table matter_codex_agent_roles enable row level security;
alter table matter_codex_agent_roles force row level security;
create policy matter_codex_agent_roles_freeze_writer on matter_codex_agent_roles
	using (matter_codex_require_cluster_admin_freeze_writer())
	with check (matter_codex_require_cluster_admin_freeze_writer());

-- +goose Down
select 1;
