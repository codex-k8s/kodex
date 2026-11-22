-- name: kubernetes_execution__plan_read :one
SELECT plan.ref,plan.plan_digest,
 EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_children child
  JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
  WHERE child.plan_id=plan.id AND child.organization_id=plan.organization_id)
FROM control_plane.kubernetes_execution_plans plan
WHERE plan.organization_id=@organization_id::uuid AND plan.invocation_id=@invocation_id::uuid
 AND plan.binding_digest=@binding_digest AND plan.snapshot_digest=@snapshot_digest;
