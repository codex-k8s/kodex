-- name: kubernetes_invocation__cancel :exec
WITH outcome AS MATERIALIZED(
 SELECT CASE WHEN EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_plans plan
 JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
 JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
 WHERE plan.invocation_id=@invocation_id::uuid AND plan.organization_id=@organization_id::uuid AND effect.state IN('ATTEMPTING','UNKNOWN'))
 THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END AS state
)
UPDATE control_plane.integration_invocations invocation
SET state=outcome.state,safe_error_code=CASE WHEN outcome.state='UNKNOWN_OUTCOME' THEN 'INTEGRATION_OUTCOME_UNKNOWN' ELSE '' END,
 lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 version=version+1,updated_at=clock_timestamp()
FROM outcome WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.state IN('READY','WAITING_APPROVAL','RUNNING');
