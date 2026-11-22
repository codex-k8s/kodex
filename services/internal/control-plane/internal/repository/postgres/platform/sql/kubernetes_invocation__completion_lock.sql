-- name: kubernetes_invocation__completion_lock :exec
SELECT effect.id FROM control_plane.kubernetes_execution_effects effect
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
WHERE plan.organization_id=@organization_id::uuid AND plan.invocation_id=@invocation_id::uuid
 AND plan.binding_digest=@binding_digest ORDER BY effect.id FOR UPDATE OF effect;
