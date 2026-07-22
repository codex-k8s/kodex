-- name: process_runs__wait_owner :exec
update matter_codex_process_runs
set status = 'waiting_owner',
	finished_at = null,
	updated_at = $2
where id = $1;
