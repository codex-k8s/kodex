-- name: RuntimeExecutionSessionHasLive
SELECT EXISTS (
    SELECT 1
    FROM control_plane.runtime_executions
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
      AND session_id = @session_id::uuid
      AND state IN ('PENDING', 'ADMITTED', 'RUNNING')
);
