-- name: kubernetes_execution__frame_insert :exec
INSERT INTO control_plane.kubernetes_execution_frames
 (stream_id,organization_id,direction,sequence,channel,payload_digest,byte_count,end_of_input,delivery_credits,grant_expires_at,created_at)
VALUES(@stream_id::uuid,@organization_id::uuid,@direction,@sequence,@channel,@payload_digest,@byte_count,@end_of_input,1,@grant_expires_at,clock_timestamp());
