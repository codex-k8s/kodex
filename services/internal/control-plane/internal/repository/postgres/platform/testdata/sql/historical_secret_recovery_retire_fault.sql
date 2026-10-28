-- name: historical_secret_recovery_retire_fault :one
SELECT 1/0 WHERE $1::text IS NOT NULL OR $2::bigint>=0;
