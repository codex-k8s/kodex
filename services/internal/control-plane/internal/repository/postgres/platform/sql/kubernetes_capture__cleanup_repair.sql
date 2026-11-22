-- name: kubernetes_capture__cleanup_repair :one
SELECT version,repair_generation,repaired_count,accepted_at
FROM control_plane.repair_kubernetes_stage_cleanup(@organization_id::uuid,@stream_id::uuid,@expected_version::bigint);
