-- name: service_credential_generation :one
SELECT credential_generation
FROM control_plane.worker_grant_high_watermarks
WHERE workload_id = $1;
