-- name: invocation__decide :exec
update matter_codex_tool_invocations
set state = $2, reason_code = $3,
	approved_at = case when $2::text = 'approved' then $4::timestamptz else null::timestamptz end,
	finished_at = case when $2::text <> 'approved' then $4::timestamptz else null::timestamptz end,
	updated_at = $4
where id = $1 and state = 'pending';
