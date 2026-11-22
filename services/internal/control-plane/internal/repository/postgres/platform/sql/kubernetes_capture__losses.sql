-- name: kubernetes_capture__losses :one
SELECT coalesce(sum(byte_count),0)::bigint,count(*) FROM control_plane.kubernetes_capture_losses
WHERE organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid AND created_at<=@sealed_at;
