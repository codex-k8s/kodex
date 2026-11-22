-- name: kubernetes_capture__loss_lock :one
SELECT stream.maximum_total_bytes,stream.logical_bytes,stream.maximum_frame_bytes
FROM control_plane.kubernetes_execution_streams stream
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
WHERE stream.id=@stream_id::uuid AND stream.organization_id=@organization_id::uuid AND stream.state='ACTIVE' AND child.operation='kubernetes.port-forward'
FOR UPDATE OF stream;
