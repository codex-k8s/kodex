-- name: kubernetes_capture__stream_record :one
SELECT stream.id::text,effect.ref,effect.generation,effect.state,effect.admission_expires_at,
 plan.snapshot_digest,child.input_digest,child.operation,pin.target_ref,pin.target_revision,
 coalesce(child.node_pin_ref,''),coalesce(child.node_pin_generation,0),stream.generation,stream.state,
 stream.hard_deadline,stream.maximum_frame_bytes,stream.maximum_total_bytes,stream.input_channels,stream.output_channels,
 coalesce(stream.handshake_digest,''),coalesce(stream.handshake_json->>'protocol',''),coalesce(stream.terminal_digest,''),stream.remote_outcome,
 control_plane.kubernetes_execution_child_grant_current(child.id,invocation.id)
  AND(child.approval_policy<>'HUMAN_EACH_EFFECT' OR EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.kubernetes_child_id=child.id AND gate.organization_id=child.organization_id AND gate.state='APPROVED')),
 clock_timestamp(),invocation.state,coalesce(invocation.lease_expires_at<=clock_timestamp(),true),plan.hard_deadline,coalesce(node.protocol,''),coalesce(node.serving_key_sha256,''),stream.handshake_json,stream.terminal_json
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
LEFT JOIN control_plane.kubernetes_node_transport_pins node ON node.ref=child.node_pin_ref AND node.generation=child.node_pin_generation
WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid
 AND invocation.resource_kind='KUBERNETES_TARGET' AND child.input_digest=@child_input_digest
 AND plan.binding_digest=@binding_digest AND stream.ref=@stream_ref AND stream.generation=@generation
;
