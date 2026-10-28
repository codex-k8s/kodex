-- name: repository_bundle__gate_operation :one
SELECT invocation.operation
FROM control_plane.owner_gates gate
JOIN control_plane.integration_invocations invocation ON invocation.id=gate.integration_invocation_id AND invocation.organization_id=gate.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.id=@gate_id::uuid AND invocation.id=@invocation_id::uuid;
