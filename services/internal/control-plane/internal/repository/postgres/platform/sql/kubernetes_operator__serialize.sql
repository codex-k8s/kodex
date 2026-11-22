-- name: kubernetes_operator__serialize :exec
SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('kodex.managed-kubernetes.operator.v1',0));
