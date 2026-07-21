-- +goose Up
-- Новая сессия уже замороженной cluster-admin роли создается атомарно вместе
-- с immutable binding. Это не разрешает новую роль или новую привязку к чату.
-- +goose StatementBegin
create function matter_codex_create_frozen_cluster_admin_session(
	candidate_session_key text,
	candidate_project_id bigint,
	candidate_chat_id bigint,
	candidate_role_id bigint,
	candidate_session_scope text,
	candidate_channel_id text,
	candidate_root_post_id text,
	candidate_openai_account_name text,
	candidate_kubernetes_namespace text,
	candidate_pod_name text,
	candidate_pvc_name text,
	candidate_token_secret_ref text,
	candidate_secret_content_sha256 text,
	candidate_secret_resource_uid text,
	candidate_secret_resource_version text,
	candidate_ttl_seconds integer,
	candidate_capabilities jsonb
)
returns boolean
language plpgsql
security definer
as $$
declare
	existing_session matter_codex_agent_sessions%rowtype;
	candidate_state jsonb;
	inserted_sessions integer;
begin
	if trim(candidate_session_key) = ''
		or trim(candidate_session_scope) = ''
		or trim(candidate_channel_id) = ''
		or candidate_root_post_id is null
		or trim(candidate_openai_account_name) = ''
		or trim(candidate_kubernetes_namespace) = ''
		or trim(candidate_pod_name) = ''
		or trim(candidate_pvc_name) = ''
		or trim(candidate_token_secret_ref) = ''
		or trim(candidate_secret_content_sha256) = ''
		or trim(candidate_secret_resource_uid) = ''
		or trim(candidate_secret_resource_version) = ''
		or candidate_ttl_seconds <= 0
		or candidate_capabilities is null
	then
		raise exception 'invalid frozen cluster-admin session input' using errcode = '22023';
	end if;

	perform pg_advisory_xact_lock(hashtextextended(candidate_session_key, 0));

	select session_row.* into existing_session
	from matter_codex_agent_sessions session_row
	where session_row.session_key = candidate_session_key;
	if found then
		if existing_session.project_id <> candidate_project_id
			or existing_session.chat_id <> candidate_chat_id
			or existing_session.role_id <> candidate_role_id
			or existing_session.session_scope <> candidate_session_scope
			or existing_session.mattermost_channel_id <> candidate_channel_id
			or existing_session.mattermost_root_post_id <> candidate_root_post_id
			or existing_session.openai_account_name <> candidate_openai_account_name
			or existing_session.kubernetes_namespace <> candidate_kubernetes_namespace
			or existing_session.pod_name <> candidate_pod_name
			or existing_session.pvc_name <> candidate_pvc_name
			or existing_session.token_secret_ref <> candidate_token_secret_ref
			or existing_session.secret_content_sha256 <> candidate_secret_content_sha256
			or existing_session.secret_resource_uid <> candidate_secret_resource_uid
			or existing_session.secret_resource_version <> candidate_secret_resource_version
			or existing_session.capabilities <> candidate_capabilities
			or not exists (
				select 1
				from matter_codex_cluster_admin_session_bindings binding
				where binding.session_key = candidate_session_key
					and binding.role_id = candidate_role_id
					and binding.project_id = candidate_project_id
					and binding.chat_id = candidate_chat_id
					and binding.mattermost_channel_id = candidate_channel_id
					and binding.privilege_state = matter_codex_cluster_admin_session_state(existing_session)
			)
		then
			raise exception 'frozen cluster-admin session identity conflicts with existing state'
				using errcode = '42501';
		end if;
		return false;
	end if;

	perform 1
	from matter_codex_projects project
	join matter_codex_agent_roles role
		on role.id = candidate_role_id
		and role.project_id = project.id
	join matter_codex_chats chat
		on chat.id = candidate_chat_id
		and chat.project_id = project.id
	join matter_codex_chat_participants participant
		on participant.chat_id = chat.id
		and participant.role_id = role.id
		and participant.enabled
	join matter_codex_cluster_admin_bindings binding
		on binding.role_id = role.id
		and binding.project_id = project.id
		and binding.chat_id = chat.id
		and binding.mattermost_channel_id = chat.mattermost_channel_id
	where project.id = candidate_project_id
		and role.enabled
		and lower(trim(role.kubernetes_access)) = 'cluster-admin'
		and chat.mattermost_channel_id = candidate_channel_id
		and matter_codex_cluster_admin_binding_exact(role.id, chat.id)
	for share of project, role, chat, participant;
	if not found then
		raise exception 'cluster-admin role and chat binding is not frozen exactly'
			using errcode = '42501';
	end if;

	if exists (
		select 1
		from matter_codex_cluster_admin_revocations revocation
		where (revocation.resource_type = 'session_key' and revocation.resource_key = candidate_session_key)
			or (revocation.resource_type = 'session_binding'
				and revocation.resource_key = candidate_role_id::text || ':' || candidate_session_key)
	) then
		raise exception 'cluster-admin session key was revoked' using errcode = '42501';
	end if;

	candidate_state := jsonb_build_object(
		'session_key', candidate_session_key,
		'project_id', candidate_project_id,
		'chat_id', candidate_chat_id,
		'role_id', candidate_role_id,
		'session_scope', candidate_session_scope,
		'mattermost_channel_id', candidate_channel_id,
		'mattermost_root_post_id', candidate_root_post_id,
		'openai_account_name', candidate_openai_account_name,
		'kubernetes_namespace', candidate_kubernetes_namespace,
		'pod_name', candidate_pod_name,
		'pvc_name', candidate_pvc_name,
		'token_secret_ref', candidate_token_secret_ref,
		'secret_content_sha256', candidate_secret_content_sha256,
		'secret_resource_uid', candidate_secret_resource_uid,
		'secret_resource_version', candidate_secret_resource_version,
		'capabilities', candidate_capabilities
	);

	insert into matter_codex_cluster_admin_session_bindings(
		role_id, project_id, chat_id, session_key, mattermost_channel_id, privilege_state
	) values (
		candidate_role_id, candidate_project_id, candidate_chat_id,
		candidate_session_key, candidate_channel_id, candidate_state
	);

	insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope,
		mattermost_channel_id, mattermost_root_post_id, openai_account_name,
		kubernetes_namespace, pod_name, pvc_name, token_secret_ref,
		secret_content_sha256, secret_resource_uid, secret_resource_version,
		ttl_seconds, capabilities, expires_at
	) values (
		candidate_session_key, candidate_project_id, candidate_chat_id, candidate_role_id,
		candidate_session_scope, candidate_channel_id, candidate_root_post_id,
		candidate_openai_account_name, candidate_kubernetes_namespace, candidate_pod_name,
		candidate_pvc_name, candidate_token_secret_ref, candidate_secret_content_sha256,
		candidate_secret_resource_uid, candidate_secret_resource_version,
		candidate_ttl_seconds, candidate_capabilities,
		now() + make_interval(secs => candidate_ttl_seconds)
	);
	get diagnostics inserted_sessions = row_count;
	if inserted_sessions <> 1 then
		raise exception 'frozen cluster-admin session insert was rejected'
			using errcode = '42501';
	end if;
	return true;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format(
		'alter function %I.matter_codex_create_frozen_cluster_admin_session(text, bigint, bigint, bigint, text, text, text, text, text, text, text, text, text, text, text, integer, jsonb) set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_create_frozen_cluster_admin_session(text, bigint, bigint, bigint, text, text, text, text, text, text, text, text, text, text, text, integer, jsonb) from public',
		trusted_schema
	);
	if runtime_role_name is not null then
		execute format(
			'grant execute on function %I.matter_codex_create_frozen_cluster_admin_session(text, bigint, bigint, bigint, text, text, text, text, text, text, text, text, text, text, text, integer, jsonb) to %I',
			trusted_schema, runtime_role_name
		);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000031 is forward-only: atomic cluster-admin session bootstrap cannot be removed safely';
end
$$;
-- +goose StatementEnd
