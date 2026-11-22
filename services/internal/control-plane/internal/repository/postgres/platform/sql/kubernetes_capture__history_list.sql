-- name: kubernetes_capture__history_list :many
SELECT capture.ref
FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.run_id=run.id AND node.organization_id=run.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
WHERE capture.organization_id=@organization_id::uuid AND run.ref=@run_ref AND node.ref=@node_ref
 AND pin.root_run_id=@root_id::uuid AND pin.root_actor_id=@actor_id::uuid
 AND capture.ref>@after_ref
ORDER BY capture.ref LIMIT @limit;
