-- name: kubernetes_capture__loss_quota :one
SELECT COALESCE(sum(byte_count),0)::bigint FROM control_plane.kubernetes_capture_losses
WHERE stream_id=@stream_id::uuid AND organization_id=@organization_id::uuid;
