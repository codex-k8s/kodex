-- name: service_credential_generation :one
SELECT GREATEST(registry.credential_generation, previous.credential_generation)
FROM control_plane.trusted_workload_generations AS registry
LEFT JOIN control_plane.worker_grant_high_watermarks AS previous USING (workload_id)
WHERE registry.workload_id = $1 AND registry.enabled;
