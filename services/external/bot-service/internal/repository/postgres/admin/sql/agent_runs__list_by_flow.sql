-- name: agent_runs__list_by_flow :many
select
	id,
	run_id,
	flow_id,
	profile_name,
	role,
	provider,
	owner,
	name,
	base_branch,
	head_branch,
	status,
	kubernetes_namespace,
	job_name,
	pvc_name,
	pr_url,
	summary,
	created_at,
	updated_at
from matter_codex_agent_runs
where flow_id = $1
order by created_at, id;
