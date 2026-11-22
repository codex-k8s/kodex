-- name: kubernetes_execution__frame_acknowledge :one
WITH origin AS MATERIALIZED(
 SELECT stream.id,stream.organization_id
 FROM control_plane.integration_invocations invocation
 JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
 JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
 JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
 JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
 WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid
  AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.input_digest=@input_digest
  AND plan.binding_digest=@binding_digest AND stream.ref=@stream_ref AND stream.generation=@generation
 FOR UPDATE OF stream)
UPDATE control_plane.kubernetes_execution_frames frame
SET acknowledged_at=coalesce(frame.acknowledged_at,clock_timestamp())
FROM origin WHERE frame.stream_id=origin.id AND frame.organization_id=origin.organization_id
 AND frame.direction=@direction AND frame.sequence=@sequence AND frame.payload_digest=@payload_digest
 AND(frame.acknowledged_at IS NOT NULL OR frame.direction='INPUT' AND EXISTS(
  SELECT 1 FROM control_plane.kubernetes_input_stages stage WHERE stage.stream_id=frame.stream_id
   AND stage.organization_id=frame.organization_id AND stage.sequence=frame.sequence AND stage.channel=frame.channel
   AND stage.payload_digest=frame.payload_digest AND stage.byte_count=frame.byte_count AND stage.end_of_input=frame.end_of_input
   AND stage.native_admitted_at IS NOT NULL) OR frame.direction='OUTPUT' AND EXISTS(
  SELECT 1 FROM control_plane.kubernetes_output_stages stage
  WHERE stage.stream_id=frame.stream_id AND stage.organization_id=frame.organization_id
   AND stage.sequence=frame.sequence AND stage.channel=frame.channel
   AND stage.payload_digest=frame.payload_digest AND stage.byte_count=frame.byte_count
   AND stage.delivery_ordinal=frame.delivery_credits AND stage.state='ACCEPTED'
   AND stage.accepted_at IS NOT NULL AND stage.object_version IS NOT NULL))
RETURNING frame.sequence;
