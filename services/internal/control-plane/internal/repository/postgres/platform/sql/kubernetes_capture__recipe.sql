-- name: kubernetes_capture__recipe :one
SELECT child.input_json FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
WHERE capture.id=@capture_id::uuid AND capture.organization_id=@organization_id::uuid
 AND child.operation=@operation AND child.input_digest=@child_input_digest;
