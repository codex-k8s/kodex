-- name: agent_flows__insert :one
insert into matter_codex_agent_flows(
	flow_id,
	status,
	provider,
	owner,
	name,
	base_branch,
	head_branch,
	title,
	task,
	attempt,
	max_attempts,
	summary
) values (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10,
	$11,
	$12
)
on conflict (flow_id) do update set
	updated_at = now()
returning
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
	summary,
	created_at,
	updated_at,
	(xmax = 0) as created;
