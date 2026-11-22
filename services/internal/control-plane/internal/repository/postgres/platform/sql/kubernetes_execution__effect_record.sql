-- name: kubernetes_execution__effect_record :one
SELECT child.id::text,child.operation,child.input_digest,child.input_json,child.risk,child.approval_policy,
 plan.hard_deadline,control_plane.kubernetes_execution_child_grant_current(child.id,plan.invocation_id)
 AND(child.approval_policy<>'HUMAN_EACH_EFFECT' OR effect.id IS NULL OR EXISTS(SELECT 1 FROM control_plane.owner_gates gate
  WHERE gate.kubernetes_child_id=child.id AND gate.organization_id=child.organization_id AND gate.state='APPROVED')),
 coalesce(effect.id::text,''),coalesce(effect.ref,''),coalesce(effect.generation,0),coalesce(effect.state,''),effect.admission_expires_at,
 coalesce(completion.result_json,''),coalesce(completion.result_digest,''),coalesce(completion.submission_digest,''),coalesce(completion.outcome,''),coalesce(completion.reason_code,''),coalesce(completion.applied,false),
 coalesce(stream.ref,''),stream.hard_deadline,coalesce(stream.generation,0),coalesce(stream.maximum_frame_bytes,0),coalesce(stream.maximum_total_bytes,0),coalesce(stream.input_channels,'{}'::text[]),coalesce(stream.output_channels,'{}'::text[]),clock_timestamp()
FROM control_plane.kubernetes_execution_plans plan
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
LEFT JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
LEFT JOIN control_plane.kubernetes_execution_completions completion ON completion.effect_id=effect.id AND completion.organization_id=effect.organization_id
LEFT JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
WHERE plan.organization_id=@organization_id::uuid AND plan.invocation_id=@invocation_id::uuid
 AND plan.binding_digest=@binding_digest AND plan.snapshot_digest=@snapshot_digest
 AND plan.ref=@plan_ref AND plan.plan_digest=@plan_digest AND child.ref=@child_ref AND child.input_digest=@child_input_digest;
