-- name: kubernetes_child_gate__detail :one
SELECT gate.id::text,gate.node_id::text,gate_node.ref,root.id::text,
 coalesce(root.project_id::text,''),coalesce(project.ref,''),gate.version,gate.state,
 gate.allowed_decisions,invocation.id::text,invocation.ref,
 plan.id::text,plan.ref,plan.plan_digest,plan.binding_digest,plan.snapshot_digest,plan.hard_deadline,
 child.id::text,child.ref,child.operation,child.approval_policy,child.input_json,child.input_digest,child.observation_json::text,
 pin.target_ref,pin.target_revision,coalesce(gate.closure_kind,''),clock_timestamp()
FROM control_plane.owner_gates gate
JOIN control_plane.kubernetes_execution_children child
 ON child.id=gate.kubernetes_child_id AND child.organization_id=gate.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation
 ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
 AND invocation.id=gate.integration_invocation_id AND invocation.resource_kind='KUBERNETES_TARGET'
JOIN control_plane.kubernetes_invocation_identity_bindings pin
 ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id
 AND root.id=gate.root_run_id AND pin.root_run_id=root.id
JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id
 AND gate_node.parent_node_id=invocation.node_id AND gate_node.run_id=invocation.run_id AND gate_node.root_run_id=root.id
 AND gate_node.type='HUMAN_GATE'
LEFT JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref;
