-- name: agent_flows__update :one
update matter_codex_agent_flows
set
	status = coalesce(nullif($2, ''), status),
	pr_url = coalesce(nullif($3, ''), pr_url),
	pr_number = case when $4 > 0 then $4 else pr_number end,
	attempt = case when $5 > 0 then $5 else attempt end,
	current_developer_run_id = coalesce(nullif($6, ''), current_developer_run_id),
	current_reviewer_run_id = coalesce(nullif($7, ''), current_reviewer_run_id),
	summary = coalesce(nullif($8, ''), summary),
	updated_at = now()
where flow_id = $1
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
	updated_at;
