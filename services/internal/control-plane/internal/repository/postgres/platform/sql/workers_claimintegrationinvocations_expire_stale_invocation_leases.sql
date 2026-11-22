-- name: workers_claimintegrationinvocations_expire_stale_invocation_leases :exec
UPDATE control_plane.integration_invocations invocation
SET state=CASE WHEN EXISTS(SELECT 1 FROM control_plane.integration_connections connection
      WHERE connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
       AND connection.definition_key='kubernetes-managed') AND NOT EXISTS(
      SELECT 1 FROM control_plane.kubernetes_execution_plans plan
      JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
      JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
      WHERE plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id) THEN 'FAILED' WHEN risk='READ' AND NOT EXISTS (
      SELECT 1 FROM control_plane.integration_connections connection
      WHERE connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
        AND connection.definition_key IN('kubernetes-managed','ssh-managed','ssh-execute')
    ) AND NOT EXISTS (
      SELECT 1 FROM control_plane.integration_personal_invocation_admissions admission
      WHERE admission.invocation_id=invocation.id AND admission.organization_id=invocation.organization_id
        AND NOT admission.recovery_read_only
    ) THEN 'READY' ELSE 'UNKNOWN_OUTCOME' END,
    safe_error_code=CASE WHEN EXISTS(SELECT 1 FROM control_plane.integration_connections connection
      WHERE connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
       AND connection.definition_key='kubernetes-managed') AND NOT EXISTS(
      SELECT 1 FROM control_plane.kubernetes_execution_plans plan
      JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
      JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
      WHERE plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id) THEN 'INTEGRATION_REQUEST_REJECTED' WHEN risk='READ' AND NOT EXISTS (
      SELECT 1 FROM control_plane.integration_connections connection
      WHERE connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
        AND connection.definition_key IN('kubernetes-managed','ssh-managed','ssh-execute')
    ) AND NOT EXISTS (
      SELECT 1 FROM control_plane.integration_personal_invocation_admissions admission
      WHERE admission.invocation_id=invocation.id AND admission.organization_id=invocation.organization_id
        AND NOT admission.recovery_read_only
    ) THEN safe_error_code ELSE 'INTEGRATION_OUTCOME_UNKNOWN' END,
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND claimed_workload=$2 AND state='RUNNING' AND lease_expires_at<=clock_timestamp()
