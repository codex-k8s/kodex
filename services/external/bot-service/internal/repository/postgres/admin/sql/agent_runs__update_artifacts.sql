-- name: agent_runs__update_artifacts :one
update matter_codex_agent_runs
set
	status = coalesce(nullif($2, ''), status),
	pr_url = coalesce(nullif($3, ''), pr_url),
	updated_at = now()
where run_id = $1
returning
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
	updated_at;
