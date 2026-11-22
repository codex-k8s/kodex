-- name: kubernetes_output__read :one
SELECT stage.ref,stage.channel,stage.payload_digest,stage.byte_count,stage.state,stage.authorized_until,
 stream.state='ACTIVE' AND invocation.state='RUNNING' AND invocation.lease_expires_at>=stage.authorized_until
 AND frame.delivery_credits=stage.delivery_ordinal AND frame.grant_expires_at=stage.authorized_until
 AND stage.authorized_until>clock_timestamp()
FROM control_plane.kubernetes_output_stages stage
JOIN control_plane.kubernetes_execution_frames frame ON frame.stream_id=stage.stream_id
 AND frame.organization_id=stage.organization_id AND frame.direction='OUTPUT' AND frame.sequence=stage.sequence
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=stage.stream_id AND stream.organization_id=stage.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid
 AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.input_digest=@input_digest
 AND invocation.generation=@generation AND invocation.claimed_workload='integration-gateway'
 AND invocation.claimed_lease_ref=@lease_ref AND invocation.claimed_fence_digest=@fence_digest
 AND invocation.kubernetes_claimed_instance=@instance AND plan.binding_digest=@binding_digest
 AND stream.ref=@stream_ref AND stream.generation=@generation
 AND stage.sequence=@sequence AND stage.delivery_ordinal=@delivery_ordinal;
