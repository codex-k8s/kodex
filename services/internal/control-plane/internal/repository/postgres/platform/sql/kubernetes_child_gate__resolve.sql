-- name: kubernetes_child_gate__resolve :exec
UPDATE control_plane.owner_gates gate
SET state=@state,decision=@decision,decision_comment=@comment,resolved_by=@actor_id::uuid,
 resolved_at=clock_timestamp(),version=gate.version+1
FROM control_plane.kubernetes_execution_children child,
 control_plane.kubernetes_execution_plans plan,control_plane.integration_invocations invocation
WHERE gate.organization_id=@organization_id::uuid AND gate.id=@gate_id::uuid
 AND gate.state='OPEN' AND gate.version=@expected_version
 AND (gate.expires_at IS NULL OR gate.expires_at>clock_timestamp())
 AND child.id=gate.kubernetes_child_id AND child.organization_id=gate.organization_id
 AND plan.id=child.plan_id AND plan.organization_id=child.organization_id AND plan.hard_deadline>clock_timestamp()
 AND invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
 AND invocation.id=gate.integration_invocation_id AND invocation.state='RUNNING'
 AND invocation.lease_ref=@lease_ref AND invocation.generation=@generation AND invocation.effect_fence_digest=@fence_digest
 AND invocation.lease_expires_at>clock_timestamp()
 AND control_plane.kubernetes_execution_child_grant_current(child.id,invocation.id);
