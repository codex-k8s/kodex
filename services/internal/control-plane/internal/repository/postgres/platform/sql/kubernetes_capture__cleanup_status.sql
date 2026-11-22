-- name: kubernetes_capture__cleanup_status :one
SELECT version,repair_generation,outstanding_count,outstanding_bytes,exhausted_count,eligible_exhausted_count
FROM control_plane.kubernetes_stage_cleanup_status(@organization_id::uuid,@stream_id::uuid);
