-- name: execution__receipt_insert :exec
insert into matter_codex_integration_test_executions(
	invocation_id, execution_id, execution_fence, arguments_sha256, result, recorded_at
)
values ($1, $2, $3, $4, $5::jsonb, $6)
on conflict (invocation_id) do nothing;
