-- name: kubernetes_input__view :one
SELECT stream.id::text,invocation.ref,stream.ref,run.ref,node.ref,stream.generation,child.operation,
 stream.state,stream.input_version,stream.input_channels,stream.closed_input_channels,stream.maximum_frame_bytes,stream.maximum_total_bytes,
 coalesce((SELECT max(frame.sequence) FROM control_plane.kubernetes_execution_frames frame WHERE frame.stream_id=stream.id AND frame.direction='INPUT' AND frame.acknowledged_at IS NOT NULL),0),
 stream.hard_deadline,child.observation_json,child.input_json,stream.input_sequence,stream.logical_bytes,stream.delivered_bytes,
 plan.binding_digest,plan.snapshot_digest,coalesce(invocation.lease_expires_at,stream.hard_deadline),clock_timestamp(),
 pin.target_ref,pin.target_revision,connection.ref,invocation.state
FROM control_plane.kubernetes_execution_streams stream
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid AND stream.ref=@stream_ref
FOR UPDATE OF stream,effect;
