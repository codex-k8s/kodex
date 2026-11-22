-- name: kubernetes_execution__children_read :many
SELECT child.ref,child.operation,child.input_digest,child.input_json,child.observation_json,
 coalesce(child.node_pin_ref,''),coalesce(child.node_pin_generation,0),
 coalesce(effect.state,CASE WHEN gate.state='OPEN' THEN 'WAITING_OWNER' WHEN gate.state IN('REJECTED','CANCELLED') THEN 'CANCELLED' ELSE 'READY' END)
FROM control_plane.kubernetes_execution_children child
LEFT JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
LEFT JOIN control_plane.owner_gates gate ON gate.kubernetes_child_id=child.id AND gate.organization_id=child.organization_id
WHERE child.organization_id=@organization_id::uuid AND child.plan_id=@plan_id::uuid
ORDER BY child.ordinal;
