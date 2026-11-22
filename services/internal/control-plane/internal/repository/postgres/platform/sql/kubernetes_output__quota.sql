-- name: kubernetes_output__quota :one
SELECT coalesce(sum(byte_count),0)::bigint,count(*) FROM control_plane.kubernetes_output_stages
WHERE organization_id=@organization_id::uuid AND payload_purged_at IS NULL;
