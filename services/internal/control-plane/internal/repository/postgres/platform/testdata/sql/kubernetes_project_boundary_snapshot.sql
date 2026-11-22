-- name: kubernetes_project_boundary_snapshot :one
SELECT project.ref,project.version,encode(public.digest(convert_to(jsonb_build_object(
 'lifecycle',project.lifecycle,'projectVersion',project.version,'rootState',root.state,'rootVersion',root.version,
 'invocationState',invocation.state,'lease',invocation.lease_ref,'fence',invocation.effect_fence_digest,
 'generation',invocation.generation)::text,'UTF8'),'sha256'),'hex')
FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE capture.ref=$1;
