-- name: ssh_compose__gate_preview :one
SELECT invocation.ssh_compose_snapshot->'target'->>'ref',
       invocation.ssh_compose_snapshot->>'mergedSha',
       invocation.ssh_compose_snapshot->>'treeSha',
       invocation.ssh_compose_snapshot->>'bundleSha256',
       invocation.ssh_compose_snapshot->'target'->>'runtimeImage'
FROM control_plane.owner_gates gate
JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=gate.integration_invocation_id
  AND invocation.organization_id=gate.organization_id AND invocation.node_id=gate_node.parent_node_id
JOIN control_plane.runs origin ON origin.id=invocation.run_id AND origin.organization_id=gate.organization_id
  AND origin.root_run_id=gate.root_run_id AND origin.project_id=gate.project_id
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref
  AND invocation.operation='ssh.compose.deploy' AND invocation.resource_kind='SSH_TARGET'
  AND invocation.ssh_compose_snapshot IS NOT NULL;
