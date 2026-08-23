-- name: commands_execute_lock_idempotency_scope :exec
SELECT pg_advisory_xact_lock(hashtextextended(concat_ws(E'\x1f',$1::text,$2::text,$3::text,$4::text),0))
