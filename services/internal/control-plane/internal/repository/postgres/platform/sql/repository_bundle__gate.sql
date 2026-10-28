-- name: repository_bundle__gate :one
SELECT invocation.operation,COALESCE(publication.ref,'')
FROM control_plane.owner_gates gate
JOIN control_plane.integration_invocations invocation ON invocation.id=gate.integration_invocation_id AND invocation.organization_id=gate.organization_id
LEFT JOIN control_plane.repository_bundle_publications publication ON publication.owner_gate_id=gate.id AND publication.organization_id=gate.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.id=@gate_id::uuid
  AND invocation.id=@invocation_id::uuid;
