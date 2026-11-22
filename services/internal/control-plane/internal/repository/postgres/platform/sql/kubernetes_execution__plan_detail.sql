-- name: kubernetes_execution__plan_detail :one
SELECT plan.id::text,plan.ref,plan.plan_digest,plan.proposal_digest,plan.child_count,plan.hard_deadline
FROM control_plane.kubernetes_execution_plans plan
WHERE plan.organization_id=@organization_id::uuid AND plan.invocation_id=@invocation_id::uuid
 AND plan.binding_digest=@binding_digest AND plan.snapshot_digest=@snapshot_digest;
