-- name: flow_version__get :one
SELECT
    id,
    flow_id,
    version,
    source_ref,
    definition_digest,
    status,
    activated_at,
    created_at
FROM agent_manager_flow_versions
WHERE id = @id;
