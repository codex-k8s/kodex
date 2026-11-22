-- name: kubernetes_file__child :one
SELECT child.id::text,child.input_digest,child.input_json,plan.id::text,plan.ref,plan.hard_deadline,
 control_plane.kubernetes_execution_child_grant_current(child.id,invocation.id),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_effects effect WHERE effect.child_id=child.id AND effect.organization_id=child.organization_id)
FROM control_plane.kubernetes_execution_children child
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
WHERE child.ref=@child_ref AND child.organization_id=@organization_id::uuid AND child.operation='kubernetes.cp.upload'
 AND plan.invocation_id=@invocation_id::uuid AND plan.binding_digest=@binding_digest AND plan.snapshot_digest=@snapshot_digest;
