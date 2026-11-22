-- name: kubernetes_output__quota_lock :exec
SELECT pg_advisory_xact_lock(hashtextextended(@organization_id::text,2145001));
