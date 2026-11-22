-- name: kubernetes_input__admit :one
UPDATE control_plane.kubernetes_input_stages SET native_admitted_at=clock_timestamp()
WHERE stream_id=@stream_id::uuid AND organization_id=@organization_id::uuid AND sequence=@sequence
 AND channel=@channel AND payload_digest=@payload_digest AND byte_count=@byte_count AND end_of_input=@end_of_input
 AND state='CLAIMED' AND native_admitted_at IS NULL AND delivery_expires_at>clock_timestamp()
RETURNING delivery_expires_at;
