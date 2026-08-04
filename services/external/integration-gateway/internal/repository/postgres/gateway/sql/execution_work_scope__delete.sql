-- name: ExecutionWorkScopeDelete
DELETE FROM integration_gateway.execution_work_scopes
 WHERE invocation_id = @invocation_id
