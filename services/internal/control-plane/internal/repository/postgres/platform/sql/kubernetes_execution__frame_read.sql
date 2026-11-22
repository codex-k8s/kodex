-- name: kubernetes_execution__frame_read :one
SELECT channel,payload_digest,byte_count,end_of_input,delivery_credits,acknowledged_at IS NOT NULL
FROM control_plane.kubernetes_execution_frames
WHERE stream_id=@stream_id::uuid AND organization_id=@organization_id::uuid
 AND direction=@direction AND sequence=@sequence FOR UPDATE;
