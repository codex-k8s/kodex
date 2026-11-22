-- name: kubernetes_capture__tool_summaries :many
SELECT capture.ref,stream.ref,capture.capture_generation,capture.version,capture.state,capture.reason_code
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
JOIN control_plane.kubernetes_output_captures capture ON capture.stream_id=stream.id AND capture.organization_id=stream.organization_id
WHERE invocation.id=@invocation_id::uuid AND invocation.organization_id=@organization_id::uuid
 AND capture.capture_generation=(SELECT max(latest.capture_generation) FROM control_plane.kubernetes_output_captures latest
  WHERE latest.stream_id=stream.id AND latest.organization_id=stream.organization_id)
ORDER BY stream.ref,capture.ref LIMIT 257;
