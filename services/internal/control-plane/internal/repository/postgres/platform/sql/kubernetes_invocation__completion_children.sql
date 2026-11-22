-- name: kubernetes_invocation__completion_children :many
SELECT child.ordinal,child.operation,coalesce(effect.state,''),coalesce(completion.outcome,''),
 coalesce(completion.applied,false),coalesce(completion.result_json,''),coalesce(completion.result_digest,''),
 coalesce(completion.reason_code,''),
 control_plane.kubernetes_execution_child_grant_current(child.id,invocation.id),
 EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.kubernetes_child_id=child.id AND gate.state='OPEN'),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream
  JOIN control_plane.kubernetes_execution_frames frame ON frame.stream_id=stream.id AND frame.organization_id=stream.organization_id
  WHERE stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
   AND frame.direction='OUTPUT' AND frame.byte_count>0),coalesce(owner_stream.ref,''),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_losses loss WHERE loss.organization_id=owner_stream.organization_id AND loss.stream_id=owner_stream.id)
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
LEFT JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
LEFT JOIN control_plane.kubernetes_execution_completions completion ON completion.effect_id=effect.id AND completion.organization_id=effect.organization_id
LEFT JOIN control_plane.kubernetes_execution_streams owner_stream ON owner_stream.effect_id=effect.id AND owner_stream.organization_id=effect.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
 AND plan.binding_digest=@binding_digest
ORDER BY child.ordinal;
