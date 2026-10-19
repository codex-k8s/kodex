-- name: project_purge_recovery__claim :many
SELECT ledger_id::text,organization_id::text,project_ref,object_key,object_version,object_etag,digest,size_bytes,generation,cleanup_deadline
FROM control_plane.project_purge_recovery_claim(@owner,@limit,@storage_identity);
