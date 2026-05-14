-- name: flow_version__create :exec
INSERT INTO agent_manager_flow_versions (
    id, flow_id, version, source_ref, definition_digest, status, activated_at, created_at
) VALUES (
    @id, @flow_id, @version, @source_ref, @definition_digest, @status, @activated_at::timestamptz, @created_at
);
