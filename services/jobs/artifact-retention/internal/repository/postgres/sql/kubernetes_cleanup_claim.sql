-- name: kubernetes_cleanup_claim :many
SELECT source_kind,ledger_id::text,object_key,object_version,object_etag,digest,size_bytes,generation,uncertain
FROM control_plane.kubernetes_stage_cleanup_claim(@owner,@batch,@lease);
