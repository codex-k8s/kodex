-- name: kubernetes_setup__serialize_actor :exec
SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('kubernetes-identity-setup:'||@organization_id||':'||@actor_id,0));
