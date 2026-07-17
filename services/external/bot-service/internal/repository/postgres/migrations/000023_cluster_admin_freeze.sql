-- +goose Up
create table if not exists matter_codex_cluster_admin_subjects (
	subject_type text not null,
	subject_key text not null,
	project_id bigint not null default 0,
	profile_name text not null,
	captured_at timestamptz not null default now(),
	primary key (subject_type, subject_key, project_id),
	constraint matter_codex_cluster_admin_subjects_type_check
		check (subject_type in ('agent_profile', 'agent_role'))
);

insert into matter_codex_cluster_admin_subjects(subject_type, subject_key, project_id, profile_name)
select 'agent_profile', name, 0, name
from matter_codex_agent_profiles
where lower(trim(kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

insert into matter_codex_cluster_admin_subjects(subject_type, subject_key, project_id, profile_name)
select 'agent_role', id::text, project_id, name
from matter_codex_agent_roles
where lower(trim(kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

create table if not exists matter_codex_cluster_admin_bindings (
	role_id bigint not null,
	project_id bigint not null,
	chat_id bigint not null,
	mattermost_channel_id text not null,
	captured_at timestamptz not null default now(),
	primary key (role_id, chat_id)
);

insert into matter_codex_cluster_admin_bindings(role_id, project_id, chat_id, mattermost_channel_id)
select participant.role_id, role.project_id, participant.chat_id, chat.mattermost_channel_id
from matter_codex_chat_participants participant
join matter_codex_agent_roles role on role.id = participant.role_id
join matter_codex_chats chat on chat.id = participant.chat_id and chat.project_id = role.project_id
where participant.enabled
	and lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

create table if not exists matter_codex_cluster_admin_session_bindings (
	role_id bigint not null,
	project_id bigint not null,
	chat_id bigint not null,
	session_key text not null,
	mattermost_channel_id text not null,
	captured_at timestamptz not null default now(),
	primary key (role_id, session_key)
);

insert into matter_codex_cluster_admin_session_bindings(
	role_id, project_id, chat_id, session_key, mattermost_channel_id
)
select session.role_id, session.project_id, session.chat_id, session.session_key, session.mattermost_channel_id
from matter_codex_agent_sessions session
join matter_codex_agent_roles role
	on role.id = session.role_id
	and role.project_id = session.project_id
join matter_codex_cluster_admin_bindings binding
	on binding.role_id = session.role_id
	and binding.project_id = session.project_id
	and binding.chat_id = session.chat_id
	and binding.mattermost_channel_id = session.mattermost_channel_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
on conflict do nothing;

-- +goose Down
drop table if exists matter_codex_cluster_admin_session_bindings;
drop table if exists matter_codex_cluster_admin_bindings;
drop table if exists matter_codex_cluster_admin_subjects;
