-- name: agent_runs__get :one
select
	id,
	run_id,
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
where run_id = $1;
