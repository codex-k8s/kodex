-- name: legacy_workflow_rework__read_slot :one
SELECT NOT EXISTS (
    SELECT 1 FROM control_plane.integration_invocations invocation
    WHERE invocation.organization_id=@organization_id::uuid AND invocation.node_id=@node_id::uuid
        AND invocation.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED')
        AND invocation.idempotency_key<>@idempotency_key
);
