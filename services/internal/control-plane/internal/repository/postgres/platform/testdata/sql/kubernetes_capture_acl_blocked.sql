-- name: kubernetes_capture_acl_blocked :one
SELECT cardinality(pg_blocking_pids($1::integer))>0;
