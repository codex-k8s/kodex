-- name: kubernetes_execution__frame_replay :exec
WITH replay AS(UPDATE control_plane.kubernetes_execution_frames
 SET delivery_credits=delivery_credits+1,grant_expires_at=@grant_expires_at
 WHERE stream_id=@stream_id::uuid AND organization_id=@organization_id::uuid AND direction='OUTPUT' AND sequence=@sequence
 RETURNING stream_id,organization_id)
UPDATE control_plane.kubernetes_execution_streams stream
SET delivered_bytes=stream.delivered_bytes+@byte_count
FROM replay WHERE stream.id=replay.stream_id AND stream.organization_id=replay.organization_id;
