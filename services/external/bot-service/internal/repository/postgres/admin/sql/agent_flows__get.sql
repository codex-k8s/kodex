-- name: agent_flows__get :one
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
where flow_id = $1;
