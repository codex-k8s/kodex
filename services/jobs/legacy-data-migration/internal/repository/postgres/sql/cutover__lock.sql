-- name: cutover__lock :one
SELECT pg_advisory_xact_lock(196, 1);
