-- name: invocation__idempotency_lock :one
select pg_advisory_xact_lock(hashtextextended($1, 0));
