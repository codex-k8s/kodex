with locked_session as (
	select sessions.*
	from matter_codex_agent_sessions sessions
	where sessions.session_key = $1
		and (
			sessions.runtime_reconcile_lease_token = ''
			or sessions.runtime_reconcile_lease_expires_at <= now()
		)
	for update
), active as (
	select turns.*
	from matter_codex_agent_session_turns turns
	join locked_session sessions on sessions.id = turns.session_id
	where turns.status = 'running'
	order by turns.started_at desc nulls last, turns.id desc
	limit 1
), selected as (
	select turns.id
	from matter_codex_agent_session_turns turns
	join locked_session sessions on sessions.id = turns.session_id
	where turns.status = 'queued'
		and not exists (select 1 from active)
		and (sessions.desired_runtime_revision_id is null or sessions.desired_runtime_revision_id = sessions.applied_runtime_revision_id)
		and (turns.runtime_revision_id is null or turns.runtime_revision_id = sessions.applied_runtime_revision_id)
	order by turns.created_at, turns.id
	for update of turns skip locked
	limit 1
), updated as (
	update matter_codex_agent_session_turns turns set
		status = 'running',
		started_at = now(),
		updated_at = now()
	from selected
	where turns.id = selected.id
	returning turns.*
), claimed as (
	select * from active
	union all
	select * from updated
), session_updated as (
	update matter_codex_agent_sessions sessions set
		status = 'running',
		active_turn_id = claimed.id,
		active_run_id = claimed.run_id,
		mattermost_root_post_id = case when claimed.mattermost_root_post_id <> '' then claimed.mattermost_root_post_id else sessions.mattermost_root_post_id end,
		last_activity_at = now(),
		expires_at = case when sessions.ttl_seconds > 0 then now() + make_interval(secs => sessions.ttl_seconds) else sessions.expires_at end,
		updated_at = now()
	from claimed
	where sessions.id = claimed.session_id
	returning sessions.id
)
select
	claimed.id,
	claimed.session_id,
	claimed.run_id,
	claimed.mattermost_channel_id,
	claimed.mattermost_root_post_id,
	claimed.mattermost_post_id,
	claimed.mattermost_status_post_id,
	claimed.mattermost_runs_post_id,
	claimed.parent_turn_ids,
	claimed.trigger_post_ids,
	claimed.initiator_user_names,
	claimed.user_id,
	claimed.user_name,
	claimed.message,
	claimed.status,
	claimed.final_message,
	claimed.error_message,
	claimed.artifacts::text,
	claimed.created_at,
	coalesce(claimed.started_at, 'epoch'::timestamptz),
	coalesce(claimed.finished_at, 'epoch'::timestamptz),
	claimed.updated_at,
	coalesce(claimed.runtime_revision_id, 0)
from claimed
join session_updated on session_updated.id = claimed.session_id
limit 1;
