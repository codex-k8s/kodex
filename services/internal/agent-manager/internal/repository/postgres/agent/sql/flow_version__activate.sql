-- name: flow_version__activate :exec
UPDATE agent_manager_flow_versions
SET status = @status,
    activated_at = @activated_at::timestamptz
WHERE id = @id;
