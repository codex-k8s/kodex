-- name: automation_owner_attention__lock_resolution :many
select
	attention.id,
	attention.status,
	attention.resolved_by_user_id,
	attention.resolved_by_post_id,
	run.id,
	run.public_id,
	run.occurrence_id,
	run.schedule_id,
	run.owner_mattermost_user_id,
	process.id
from matter_codex_owner_attention_requests attention
join matter_codex_scheduled_runs run
	on run.id = attention.automation_scheduled_run_id
	and run.runtime_turn_id = attention.turn_id
	and run.project_id = attention.automation_project_id
join matter_codex_schedule_occurrences occurrence
	on occurrence.id = run.occurrence_id
	and occurrence.project_id = run.project_id
join matter_codex_process_turns process_turn
	on process_turn.process_run_id = attention.process_run_id
	and process_turn.turn_id = attention.turn_id
join matter_codex_process_runs process
	on process.id = attention.process_run_id
	and process.project_id = attention.automation_project_id
	and process.policy_revision_id = attention.automation_policy_revision_id
	and process.root_initiator_user_id = attention.automation_root_initiator_user_id
where run.project_id = $1
	and attention.request_kind = 'automation'
	and attention.mattermost_post_id <> ''
	and attention.automation_mattermost_post_create_at is not null
	and $6::bigint > attention.automation_mattermost_post_create_at
	and process.root_initiator_user_id = $2
	and attention.automation_mattermost_channel_id = $3
	and attention.automation_mattermost_root_post_id = $4
	and (
		(
			attention.status = 'resolved'
			and attention.resolved_by_user_id = $2
			and attention.resolved_by_post_id = $5
			and attention.automation_resolved_by_post_create_at = $6
		)
		or (
			attention.status = 'open'
			and not exists (
				select 1
				from matter_codex_owner_attention_requests replay
				where replay.automation_project_id = $1
					and replay.automation_root_initiator_user_id = $2
					and replay.automation_mattermost_channel_id = $3
					and replay.automation_mattermost_root_post_id = $4
					and replay.status = 'resolved'
					and replay.resolved_by_user_id = $2
					and replay.resolved_by_post_id = $5
			)
		)
	)
order by case when attention.status = 'resolved' then 0 else 1 end, attention.id
limit 2
for update of attention, run, occurrence, process;
