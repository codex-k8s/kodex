-- name: kubernetes_input__list :many
SELECT invocation.ref,stream.ref FROM control_plane.integration_invocations invocation
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND run.ref=@run_ref AND node.ref=@node_ref
 AND pin.root_actor_id=@actor_id::uuid AND root.initiated_by=pin.root_actor_id AND pin.root_run_id=root.id
 AND stream.ref>@after_ref ORDER BY stream.ref LIMIT @limit;
