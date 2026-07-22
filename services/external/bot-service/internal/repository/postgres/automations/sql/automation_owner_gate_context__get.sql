-- name: automation_owner_gate_context__get :one
select
	run.id,
	run.public_id,
	run.project_id,
	coalesce(run.runtime_turn_id, 0),
	process.id,
	process.public_id,
	process.policy_revision_id,
	process.root_initiator_user_id,
	process.root_initiator_user_name,
	run.mattermost_channel_id,
	run.mattermost_root_post_id
from matter_codex_scheduled_runs run
join matter_codex_process_turns process_turn
	on process_turn.turn_id = run.runtime_turn_id
join matter_codex_process_runs process
	on process.id = process_turn.process_run_id
	and process.project_id = run.project_id
join matter_codex_policy_revisions policy
	on policy.id = process.policy_revision_id
	and policy.project_id = process.project_id
where run.public_id = $1
	and run.project_id = $2
	and run.runtime_session_id = $3
	and run.runtime_session_key = $4
	and run.runtime_turn_id is not null
	and length(trim(process.root_initiator_user_id)) > 0
	and length(trim(run.mattermost_channel_id)) > 0
	and length(trim(run.mattermost_root_post_id)) > 0
for key share of run, process_turn, process;
