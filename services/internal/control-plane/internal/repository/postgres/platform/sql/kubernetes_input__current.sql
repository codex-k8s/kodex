-- name: kubernetes_input__current :one
SELECT control_plane.kubernetes_execution_child_grant_current(child.id,plan.invocation_id)
 AND (child.approval_policy<>'HUMAN_EACH_EFFECT' OR EXISTS(SELECT 1 FROM control_plane.owner_gates gate
 WHERE gate.kubernetes_child_id=child.id AND gate.organization_id=child.organization_id AND gate.state='APPROVED'))
 AND (child.node_pin_ref IS NULL OR EXISTS(SELECT 1 FROM control_plane.kubernetes_node_transport_pins pin
 WHERE pin.ref=child.node_pin_ref AND pin.generation=child.node_pin_generation
 AND (pin.payload::jsonb->>'expiresAt')::timestamptz>clock_timestamp()
 AND (pin.payload::jsonb->>'notBefore')::timestamptz<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_retired_node_pins retired WHERE retired.ref=pin.ref)))
FROM control_plane.kubernetes_execution_streams stream
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
WHERE stream.id=@stream_id::uuid AND stream.organization_id=@organization_id::uuid AND effect.state='ATTEMPTING';
