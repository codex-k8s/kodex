-- name: interaction_admission__resource :one
select case
	when $1 not in ('mattermost.callback.action', 'mattermost.callback.dialog') then false
	when $8 <> 'single-installation' then false
	when $9 = '' then false
	when $5 = '' or $6 = '' or $7 = '' then false
	when not exists(
		select 1 from matter_codex_chats where mattermost_channel_id = $6
	) then false
	when $9 <> 'installation-root' and (
		$9 !~ '^[1-9][0-9]*$'
		or not exists(
			select 1 from matter_codex_chats
			where mattermost_channel_id = $6 and project_id::text = $9
		)
	) then false
	when $10 <> '' and not exists(
		select 1 from matter_codex_agent_sessions
		where session_key = $10
			and mattermost_channel_id = $6
			and ($9 = 'installation-root' or project_id::text = $9)
	) then false
	when $3 = '' then $4 = ''
	when $4 = '' then $3 in (
		'project', 'repository', 'agent_role', 'chat', 'project_runtime_var',
		'openai_account', 'github_account', 'profile', 'prompt_template', 'run', 'system', 'runtime'
	)
	when $3 = 'system' or $3 = 'runtime' then false
	when $3 = 'project' then exists(
		select 1 from matter_codex_projects
		where id::text = $4 and ($9 = 'installation-root' or id::text = $9)
	)
	when $3 = 'agent_role' and $2 like '%;action=list;%' then exists(
		select 1 from matter_codex_projects
		where id::text = $4 and ($9 = 'installation-root' or id::text = $9)
	)
	when $3 = 'agent_role' then exists(
		select 1 from matter_codex_agent_roles
		where id::text = $4 and ($9 = 'installation-root' or project_id::text = $9)
	)
	when $3 = 'chat' and $2 like '%;action=list;%' then exists(
		select 1 from matter_codex_projects
		where id::text = $4 and ($9 = 'installation-root' or id::text = $9)
	)
	when $3 = 'chat' then exists(
		select 1 from matter_codex_chats
		where id::text = $4 and ($9 = 'installation-root' or project_id::text = $9)
	)
	when $3 = 'project_runtime_var' and $2 like '%;action=list;%' then exists(
		select 1 from matter_codex_projects
		where id::text = $4 and ($9 = 'installation-root' or id::text = $9)
	)
	when $3 = 'project_runtime_var' then exists(
		select 1 from matter_codex_project_runtime_variables
		where id::text = $4 and ($9 = 'installation-root' or project_id::text = $9)
	)
	when $3 = 'thread_context' then exists(
		select 1 from matter_codex_thread_contexts
		where id::text = $4 and mattermost_channel_id = $6
			and ($9 = 'installation-root' or project_id::text = $9)
	)
	when $3 = 'openai_account' and $9 = 'installation-root' then exists(
		select 1 from matter_codex_openai_accounts where name = $4
	)
	when $3 = 'github_account' and $9 = 'installation-root' then exists(
		select 1 from matter_codex_github_accounts where name = $4
	)
	when $3 = 'profile' and $9 = 'installation-root' then exists(
		select 1 from matter_codex_agent_profiles where name = $4
	)
	when $3 = 'prompt_template' and $9 = 'installation-root' then exists(
		select 1 from matter_codex_agent_prompt_templates
		where profile_name = split_part($4, '/', 1)
			and template_key = split_part($4, '/', 2)
			and split_part($4, '/', 3) = ''
	)
	when $3 = 'run' and $9 = 'installation-root' then exists(
		select 1 from matter_codex_agent_runs where run_id = $4
	)
	when $3 = 'repository' and position(':' in $4) > 1 then exists(
		select 1 from matter_codex_repositories
		where provider = split_part($4, ':', 1)
			and owner = split_part(split_part($4, ':', 2), '/', 1)
			and name = split_part(split_part($4, ':', 2), '/', 2)
			and (
				$9 = 'installation-root'
				or exists(
					select 1 from matter_codex_project_repositories
					where repository_id = matter_codex_repositories.id and project_id::text = $9
				)
			)
	)
	when $3 = 'repository' and $9 = 'installation-root' and (
		$2 like '%;action=repository_branches;%' or $2 like '%;action=repository_connect;%'
	) then length($4) between 16 and 4096
	when $3 = 'agent_session_turn' then exists(
		select 1
		from matter_codex_agent_session_turns turn
		join matter_codex_agent_sessions session on session.id = turn.session_id
		where turn.id::text = $4
			and turn.user_id = $5
			and turn.mattermost_channel_id = $6
			and session.session_key = $10
			and session.project_id::text = $9
	)
	else false
end;
