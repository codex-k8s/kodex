-- name: kubernetes_file__cleanup_repair :one
SELECT version,repair_generation,repaired_count,accepted_at
FROM control_plane.repair_kubernetes_file_source_cleanup(@organization_id::uuid,@source_id::uuid,@expected_version);
