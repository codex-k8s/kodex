-- name: kubernetes_file__cleanup_status :one
SELECT source.cleanup_version,source.cleanup_repair_generation,
 CASE WHEN source.payload_purged_at IS NULL THEN 1 ELSE 0 END::bigint,
 CASE WHEN source.payload_purged_at IS NULL THEN coalesce(source.byte_count,source.maximum_bytes) ELSE 0 END::bigint,
 CASE WHEN source.payload_purged_at IS NULL AND source.cleanup_attempts=3 THEN 1 ELSE 0 END::bigint,
 CASE WHEN source.payload_purged_at IS NULL AND source.cleanup_attempts=3
  AND(source.cleanup_deadline IS NULL OR source.cleanup_deadline<=clock_timestamp())
  AND control_plane.kubernetes_file_source_cleanup_eligible(source.id,source.organization_id) THEN 1 ELSE 0 END::bigint
FROM control_plane.kubernetes_file_input_sources source
WHERE source.id=@source_id::uuid AND source.organization_id=@organization_id::uuid;
