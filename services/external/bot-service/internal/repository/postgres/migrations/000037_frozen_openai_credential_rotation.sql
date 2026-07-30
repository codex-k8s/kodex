-- +goose Up
-- Rotation never mutates an existing Secret. The Kubernetes adapter first
-- creates a verified immutable revision, then this function atomically switches
-- already-frozen dependencies to its exact identity.
-- +goose StatementBegin
create function matter_codex_rotate_frozen_openai_credential(
	candidate_account_name text,
	candidate_secret_ref text,
	candidate_secret_content_sha256 text,
	candidate_secret_resource_uid text,
	candidate_secret_resource_version text,
	candidate_actor_user_id text,
	candidate_actor_user_name text
)
returns matter_codex_openai_accounts
language plpgsql
security definer
as $$
declare
	current_account matter_codex_openai_accounts%rowtype;
	current_credential matter_codex_credentials%rowtype;
begin
	candidate_account_name := trim(coalesce(candidate_account_name, ''));
	candidate_secret_ref := trim(coalesce(candidate_secret_ref, ''));
	candidate_secret_content_sha256 := trim(coalesce(candidate_secret_content_sha256, ''));
	candidate_secret_resource_uid := trim(coalesce(candidate_secret_resource_uid, ''));
	candidate_secret_resource_version := trim(coalesce(candidate_secret_resource_version, ''));
	candidate_actor_user_id := trim(coalesce(candidate_actor_user_id, ''));
	candidate_actor_user_name := trim(coalesce(candidate_actor_user_name, ''));
	if candidate_account_name = ''
		or candidate_secret_ref = ''
		or candidate_secret_content_sha256 !~ '^[a-f0-9]{64}$'
		or candidate_secret_resource_uid = ''
		or candidate_secret_resource_version = ''
	then
		raise exception 'invalid frozen OpenAI credential rotation input' using errcode = '22023';
	end if;

	perform pg_advisory_xact_lock(hashtextextended('frozen-openai-rotation:' || candidate_account_name, 0));

	select account.* into current_account
	from matter_codex_openai_accounts account
	where account.name = candidate_account_name
	for update;
	if not found then
		raise exception 'frozen OpenAI account was not found' using errcode = 'P0002';
	end if;

	select credential.* into current_credential
	from matter_codex_credentials credential
	where credential.id = current_account.credential_id
	for update;
	if not found then
		raise exception 'frozen OpenAI credential was not found' using errcode = 'P0002';
	end if;

	if not exists (
		select 1
		from matter_codex_cluster_admin_dependencies dependency
		where dependency.resource_type = 'openai_account'
			and dependency.resource_key = candidate_account_name
	) and not exists (
		select 1
		from matter_codex_cluster_admin_subjects subject
		join matter_codex_agent_profiles profile
			on subject.subject_type = 'agent_profile'
			and subject.subject_key = profile.name
		where profile.openai_account_name = candidate_account_name
			and lower(trim(profile.kubernetes_access)) = 'cluster-admin'
	) then
		raise exception 'OpenAI account has no frozen cluster-admin dependency' using errcode = '42501';
	end if;

	if exists (
		select 1
		from matter_codex_agent_sessions session_row
		join matter_codex_agent_session_turns active_turn
			on active_turn.id = session_row.active_turn_id
		where session_row.openai_account_name = candidate_account_name
			and active_turn.status in ('queued', 'running', 'capacity_retry')
	) then
		raise exception 'OpenAI account is used by an active agent turn' using errcode = '55006';
	end if;

	if exists (
		select 1
		from matter_codex_cluster_admin_revocations revocation
		join matter_codex_cluster_admin_dependencies dependency
			on dependency.resource_type = 'openai_account'
			and dependency.resource_key = candidate_account_name
			and revocation.resource_type = 'dependency'
			and revocation.resource_key =
				dependency.role_id::text || ':openai_account:' || candidate_account_name
	) or exists (
		select 1
		from matter_codex_cluster_admin_revocations revocation
		join matter_codex_cluster_admin_subjects subject
			on subject.subject_type = 'agent_profile'
			and subject.privilege_state ->> 'openai_account_name' = candidate_account_name
			and revocation.resource_type = 'profile_dependency'
			and revocation.resource_key =
				subject.subject_key || ':openai_account:' || candidate_account_name
	) then
		raise exception 'revoked cluster-admin dependency cannot be rotated' using errcode = '42501';
	end if;

	-- Remove only snapshots of this logical account inside the current
	-- transaction. Concurrent admissions continue to observe the old committed
	-- snapshot and wait on account/credential row locks until the atomic switch.
	delete from matter_codex_cluster_admin_dependencies dependency
	where dependency.resource_type = 'openai_account'
		and dependency.resource_key = candidate_account_name;

	delete from matter_codex_cluster_admin_subjects subject
	using matter_codex_agent_profiles profile
	where subject.subject_type = 'agent_profile'
		and subject.subject_key = profile.name
		and profile.openai_account_name = candidate_account_name
		and lower(trim(profile.kubernetes_access)) = 'cluster-admin';

	update matter_codex_credentials credential
	set
		secret_ref = candidate_secret_ref,
		secret_content_sha256 = candidate_secret_content_sha256,
		secret_resource_uid = candidate_secret_resource_uid,
		secret_resource_version = candidate_secret_resource_version,
		status = 'authorized',
		last_checked_at = now(),
		updated_at = now()
	where credential.id = current_credential.id;

	update matter_codex_openai_accounts account
	set status = 'authorized', updated_at = now()
	where account.id = current_account.id
	returning account.* into current_account;

	insert into matter_codex_cluster_admin_dependencies(
		role_id, resource_type, resource_key, privilege_state, captured_at
	)
	select
		role.id,
		'openai_account',
		candidate_account_name,
		matter_codex_cluster_admin_openai_account_state(candidate_account_name),
		now()
	from matter_codex_agent_roles role
	where lower(trim(role.kubernetes_access)) = 'cluster-admin'
		and role.openai_account_name = candidate_account_name;

	insert into matter_codex_cluster_admin_subjects(
		subject_type, subject_key, project_id, profile_name, privilege_state, captured_at
	)
	select
		'agent_profile',
		profile.name,
		0,
		profile.name,
		matter_codex_cluster_admin_profile_state(profile),
		now()
	from matter_codex_agent_profiles profile
	where lower(trim(profile.kubernetes_access)) = 'cluster-admin'
		and profile.openai_account_name = candidate_account_name;

	insert into matter_codex_audit_events(
		event_type, actor_user_id, actor_user, resource_type, resource_name, summary
	) values (
		'openai.account.frozen_credential_rotated',
		candidate_actor_user_id,
		candidate_actor_user_name,
		'openai',
		candidate_account_name,
		'frozen OpenAI credential switched to a verified immutable Secret revision'
	);

	return current_account;
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
		'alter function %I.matter_codex_rotate_frozen_openai_credential(text, text, text, text, text, text, text) set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_rotate_frozen_openai_credential(text, text, text, text, text, text, text) from public',
		trusted_schema
	);
	if runtime_role_name is not null then
		execute format(
			'grant execute on function %I.matter_codex_rotate_frozen_openai_credential(text, text, text, text, text, text, text) to %I',
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
	raise exception 'migration 000037 is forward-only: frozen credential rotation audit and revisions cannot be removed safely';
end
$$;
-- +goose StatementEnd
