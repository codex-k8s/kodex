-- name: legacy_workflow_rework__read_slot :one
SELECT NOT EXISTS (
    SELECT 1 FROM control_plane.integration_invocations invocation
    JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id
        AND gate.organization_id=invocation.organization_id
    WHERE invocation.organization_id=@organization_id::uuid AND invocation.node_id=@node_id::uuid
        AND invocation.idempotency_key<>@idempotency_key
);
