-- name: ReceiptLock
SELECT pg_advisory_xact_lock(hashtextextended(@key_hash, 0))
