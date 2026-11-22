-- name: kubernetes_execution__frame_stream :one
SELECT stream.id::text,stream.state,least(stream.hard_deadline,@lease_expires_at::timestamptz),
 stream.maximum_frame_bytes,stream.maximum_total_bytes,stream.logical_bytes,stream.delivered_bytes,
 stream.input_sequence,stream.output_sequence,stream.input_channels,stream.output_channels,@channel=ANY(stream.closed_input_channels),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_frames frame WHERE frame.stream_id=stream.id AND frame.direction='INPUT' AND frame.sequence=stream.input_sequence AND frame.acknowledged_at IS NULL),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_frames frame WHERE frame.stream_id=stream.id AND frame.direction='OUTPUT' AND frame.sequence=stream.output_sequence AND frame.acknowledged_at IS NULL),clock_timestamp()
FROM control_plane.kubernetes_execution_streams stream
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
WHERE stream.ref=@stream_ref AND stream.organization_id=@organization_id::uuid
 AND plan.invocation_id=@invocation_id::uuid AND plan.binding_digest=@binding_digest AND plan.snapshot_digest=@snapshot_digest
 AND control_plane.kubernetes_execution_child_grant_current(child.id,plan.invocation_id)
 AND (child.approval_policy<>'HUMAN_EACH_EFFECT' OR EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.kubernetes_child_id=child.id AND gate.organization_id=child.organization_id AND gate.state='APPROVED'))
 AND stream.generation=@generation AND stream.hard_deadline>clock_timestamp() AND effect.state='ATTEMPTING'
 AND (child.node_pin_ref IS NULL OR EXISTS(SELECT 1 FROM control_plane.kubernetes_node_transport_pins pin WHERE pin.ref=child.node_pin_ref AND pin.generation=child.node_pin_generation
 AND (pin.payload::jsonb->>'expiresAt')::timestamptz>clock_timestamp() AND (pin.payload::jsonb->>'notBefore')::timestamptz<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_retired_node_pins retired WHERE retired.ref=pin.ref)))
FOR UPDATE OF stream,effect;
