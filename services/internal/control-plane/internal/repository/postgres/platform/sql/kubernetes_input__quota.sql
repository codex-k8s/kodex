-- name: kubernetes_input__quota :one
SELECT coalesce(sum(byte_count),0),count(*) FROM control_plane.kubernetes_input_stages WHERE organization_id=@organization_id::uuid AND payload_purged_at IS NULL AND channel<>'FILE_CONTENT';
