-- name: kubernetes_file__prepared_mutex :exec
SELECT pg_advisory_xact_lock(hashtextextended('kodex.kubernetes.file.source:'||@organization_id::uuid::text||':'||@source_ref,0));
