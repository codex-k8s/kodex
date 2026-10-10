-- name: prepared_claim :many
SELECT ledger_id::text,object_key,object_version,object_etag,digest,size_bytes,generation,uncertain
FROM control_plane.prepared_content_claim(@owner,@batch,@lease);
