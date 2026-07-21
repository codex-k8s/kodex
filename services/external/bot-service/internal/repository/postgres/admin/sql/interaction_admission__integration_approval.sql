-- name: interaction_admission__integration_approval :one
select case
	when $1 <> 'mattermost.callback.action' then false
	when $2 not in ('action;kind=integration_approval;action=approve', 'action;kind=integration_approval;action=reject') then false
	when $8 <> 'single-installation' then false
	when $3 <> 'integration_approval' then false
	when $4 = '' or $5 = '' or $6 = '' or $7 = '' or $9 = '' or $10 = '' then false
	when exists(
		select 1 from matter_codex_mattermost_bot_identities where mattermost_user_id = $5
	) then false
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
	else exists(
		select 1
		from matter_codex_approval_requests approval
		join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
		join matter_codex_agent_sessions session on session.id = invocation.session_id
		where approval.public_id = $4
			and approval.exact_approver_user_id = $5
			and approval.mattermost_channel_id = $6
			and approval.mattermost_post_id = $7
			and approval.state in ('pending', 'approved', 'rejected')
			and invocation.project_id::text = $9
			and invocation.session_id = session.id
			and session.session_key = $10
	)
end;
