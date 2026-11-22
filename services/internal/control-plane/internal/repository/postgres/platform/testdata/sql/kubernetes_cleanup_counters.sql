-- name: kubernetes_cleanup_counters :one
SELECT cleanup_attempts,cleanup_total_attempts,cleanup_repair_generation,object_version,object_etag,payload_purged_at IS NOT NULL
FROM control_plane.kubernetes_output_stages WHERE id=$1 AND $2::text='KUBERNETES_OUTPUT'
UNION ALL SELECT cleanup_attempts,cleanup_total_attempts,cleanup_repair_generation,object_version,object_etag,payload_purged_at IS NOT NULL
FROM control_plane.kubernetes_input_stages WHERE id=$1 AND $2::text='KUBERNETES_INPUT'
UNION ALL SELECT cleanup_attempts,cleanup_total_attempts,cleanup_repair_generation,coalesce(object_version,''),coalesce(object_etag,''),deletion_receipt_digest IS NOT NULL
FROM control_plane.prepared_content WHERE id=$1 AND $2::text='KUBERNETES_PREPARED';
