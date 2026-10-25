-- name: historical_secret_recovery_intent_fault :one
SELECT 1/0 WHERE @operation_id::text IS NOT NULL OR @encrypted::text IS NOT NULL OR @materialization::text IS NOT NULL;
