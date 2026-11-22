-- name: artifact_retention_issuer_head :one
SELECT max(version_id), EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='ira_artifact_retention_issuer_g1')
FROM public.goose_db_version WHERE is_applied AND current_database()='internal_rpc_authority';
