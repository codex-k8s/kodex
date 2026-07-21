-- name: execution__receipt_get :one
select execution_id, execution_fence, encode(arguments_sha256, 'hex'),
	result ->> 'namespace', result ->> 'workload_kind', result ->> 'workload_name', result ->> 'recorded_at'
from matter_codex_integration_test_executions
where invocation_id = $1;
