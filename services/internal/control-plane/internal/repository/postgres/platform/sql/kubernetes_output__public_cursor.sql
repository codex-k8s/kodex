-- name: kubernetes_output__public_cursor :one
SELECT stream.output_sequence,child.input_digest,clock_timestamp()
FROM control_plane.kubernetes_execution_streams stream
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
WHERE stream.organization_id=@organization_id::uuid AND stream.id=@stream_id::uuid;
