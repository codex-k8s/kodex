-- name: kubernetes_capture__invocation :many
SELECT capture.id::text,capture.organization_id::text,capture.stream_id::text,capture.ref,capture.source_snapshot,
 capture.snapshot_digest,capture.capture_generation,capture.state,coalesce(capture.capture_kind,''),capture.reason_code,
 coalesce(capture.source_digest,''),coalesce(capture.acknowledged_prefix,0),coalesce(capture.dropped_bytes,0)
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
JOIN control_plane.kubernetes_output_captures capture ON capture.stream_id=stream.id AND capture.organization_id=stream.organization_id AND capture.capture_generation=stream.capture_generation
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.generation=@generation AND plan.binding_digest=@binding_digest
ORDER BY child.ordinal;
