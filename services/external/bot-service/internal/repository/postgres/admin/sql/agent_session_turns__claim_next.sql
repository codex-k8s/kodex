with active as (
	select turns.*
	from matter_codex_agent_session_turns turns
	join matter_codex_agent_sessions sessions on sessions.id = turns.session_id
	where sessions.session_key = $1 and turns.status = 'running'
	order by turns.started_at desc nulls last, turns.id desc
	limit 1
), selected as (
	select turns.id
	from matter_codex_agent_session_turns turns
	join matter_codex_agent_sessions sessions on sessions.id = turns.session_id
	where sessions.session_key = $1 and turns.status = 'queued'
		and not exists (select 1 from active)
	order by turns.created_at, turns.id
	for update skip locked
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
)
select
	id,
	session_id,
	run_id,
	mattermost_channel_id,
	mattermost_root_post_id,
	mattermost_post_id,
	mattermost_status_post_id,
	user_id,
	user_name,
	message,
	status,
	final_message,
	error_message,
	artifacts::text,
	created_at,
	coalesce(started_at, 'epoch'::timestamptz),
	coalesce(finished_at, 'epoch'::timestamptz),
	updated_at
from claimed
limit 1;
