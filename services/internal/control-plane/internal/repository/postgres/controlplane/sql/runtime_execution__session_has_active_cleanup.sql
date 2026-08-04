-- name: RuntimeExecutionSessionHasActiveCleanup :one
-- PostgreSQL owner transaction использует тот же session graph lock до вызова.
SELECT EXISTS (
    SELECT 1
    FROM control_plane.runtime_executions
    WHERE organization_id = @organization_id
      AND project_id = @project_id
      AND session_id = @session_id
      AND cleanup_authorization_state = 'ACTIVE'
);
