-- name: kubernetes_invocation__continuation_gate :one
-- Выбирает подтверждённое решение точного плана; пустой результат сохраняет родительский путь.
SELECT coalesce((SELECT gate.ref
 FROM control_plane.integration_invocations invocation
 JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id
  AND plan.organization_id=invocation.organization_id
 JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id
  AND child.organization_id=plan.organization_id
 JOIN control_plane.owner_gates gate ON gate.kubernetes_child_id=child.id
  AND gate.organization_id=invocation.organization_id AND gate.integration_invocation_id=invocation.id
 JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
 WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
  AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.state IN ('SUCCEEDED','FAILED')
  AND gate.root_run_id=run.root_run_id AND gate.project_id IS NOT DISTINCT FROM run.project_id
  AND gate.state='APPROVED' AND gate.decision='APPROVE'
 ORDER BY gate.resolved_at DESC,gate.created_at DESC,gate.ref LIMIT 1),'');
