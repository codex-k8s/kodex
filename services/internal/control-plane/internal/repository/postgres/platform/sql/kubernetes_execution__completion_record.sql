-- name: kubernetes_execution__completion_record :one
SELECT effect.id::text,effect.state,child.operation,child.id::text,plan.snapshot_digest,least(plan.hard_deadline,coalesce(stream.hard_deadline,effect.admission_expires_at)),
 control_plane.kubernetes_execution_child_grant_current(child.id,invocation.id)
 AND(child.approval_policy<>'HUMAN_EACH_EFFECT' OR EXISTS(SELECT 1 FROM control_plane.owner_gates gate
  WHERE gate.kubernetes_child_id=child.id AND gate.organization_id=child.organization_id AND gate.state='APPROVED')),
 coalesce(stream.ref,''),coalesce(stream.state,''),stream.terminal_json,
 coalesce(completion.result_json,''),coalesce(completion.result_digest,''),coalesce(completion.submission_digest,''),coalesce(completion.outcome,''),coalesce(completion.reason_code,''),coalesce(completion.applied,false),clock_timestamp()
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
LEFT JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
LEFT JOIN control_plane.kubernetes_execution_completions completion ON completion.effect_id=effect.id AND completion.organization_id=effect.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.input_digest=@invocation_input_digest AND plan.binding_digest=@binding_digest
 AND effect.ref=@effect_ref AND effect.generation=@effect_generation AND child.input_digest=@child_input_digest
FOR UPDATE OF effect;
