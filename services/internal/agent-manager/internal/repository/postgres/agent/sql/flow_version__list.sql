-- name: flow_version__list :many
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
WHERE flow_id = @flow_id
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY version DESC, id
LIMIT @limit::integer
OFFSET @offset::integer;
