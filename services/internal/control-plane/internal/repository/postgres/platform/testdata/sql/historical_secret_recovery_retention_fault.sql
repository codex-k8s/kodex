-- name: historical_secret_recovery_retention_fault :one
SELECT 1/0 WHERE $1::text IS NOT NULL OR $2::text IS NOT NULL OR $3::bigint>=0;
