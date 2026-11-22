-- name: kubernetes_child_gate__origin :one
SELECT gate.kubernetes_child_id IS NOT NULL,coalesce(invocation.ref,'')
FROM control_plane.owner_gates gate
LEFT JOIN control_plane.integration_invocations invocation
 ON invocation.id=gate.integration_invocation_id AND invocation.organization_id=gate.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref;
