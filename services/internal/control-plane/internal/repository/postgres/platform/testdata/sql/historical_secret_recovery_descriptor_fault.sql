-- name: historical_secret_recovery_descriptor_fault :one
SELECT 1/0 WHERE @secret_id::text IS NOT NULL OR @revision::bigint>=0;
