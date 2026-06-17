-- name: agent_flows__list :many
select
	id,
	flow_id,
	status,
	provider,
	owner,
	name,
	base_branch,
	head_branch,
	title,
	task,
	pr_url,
	pr_number,
	attempt,
	max_attempts,
	developer_profile_name,
	reviewer_profile_name,
	flow_preset,
	current_developer_run_id,
	current_reviewer_run_id,
	owner_user_id,
	owner_user,
	control_channel_id,
	control_post_id,
	action_token,
	owner_decision,
	summary,
	created_at,
	updated_at
from matter_codex_agent_flows
where $1 = '' or status = $1
order by updated_at desc, id desc
limit $2;
