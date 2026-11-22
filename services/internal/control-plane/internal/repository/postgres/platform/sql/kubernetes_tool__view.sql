-- name: kubernetes_tool__view :one
SELECT invocation.ref,run.ref,node.ref,connection.ref,pin.target_ref,pin.target_revision,
 invocation.operation,invocation.state,invocation.version,
 EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.organization_id=invocation.organization_id
  AND gate.integration_invocation_id=invocation.id AND gate.state='OPEN'),coalesce(run.project_id::text,''),
 coalesce((SELECT gate.ref FROM control_plane.owner_gates gate
  JOIN control_plane.kubernetes_execution_children child ON child.id=gate.kubernetes_child_id AND child.organization_id=gate.organization_id
  JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id AND plan.invocation_id=invocation.id
  WHERE gate.organization_id=invocation.organization_id AND gate.integration_invocation_id=invocation.id
   AND gate.state IN ('OPEN','APPROVED') AND gate.kubernetes_child_id IS NOT NULL
  ORDER BY (gate.state='OPEN') DESC,gate.created_at DESC,gate.ref LIMIT 1),''),invocation.safe_error_code
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
 AND invocation.resource_kind='KUBERNETES_TARGET';
