update matter_codex_agent_sessions session_row set
	runtime_reconcile_lease_token = $5,
	runtime_reconcile_lease_revision_id = case when $2 > 0 then $2 else null end,
	runtime_reconcile_lease_expires_at = now() + make_interval(secs => $6::int),
	updated_at = now()
where session_row.session_key = $1
	and session_row.active_turn_id is null
	and coalesce(session_row.desired_runtime_revision_id, 0) = $2
	and coalesce(session_row.applied_runtime_revision_id, 0) = $3
	and session_row.applied_pod_uid = $4
	and (
		session_row.runtime_reconcile_lease_token = ''
		or session_row.runtime_reconcile_lease_expires_at <= now()
	)
	and not exists (
		select 1 from matter_codex_agent_session_turns running_turn
		where running_turn.session_id = session_row.id and running_turn.status = 'running'
	)
returning
	id,
	session_key,
	coalesce(desired_runtime_revision_id, 0),
	coalesce(applied_runtime_revision_id, 0),
	applied_pod_uid,
	runtime_reconcile_lease_token,
	coalesce(runtime_reconcile_lease_expires_at, 'epoch'::timestamptz);
-- name: agent_sessions__acquire_runtime_lease :one
