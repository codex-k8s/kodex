-- name: prepared_content_replay :one
SELECT id::text,ref,generation,prepared_artifact_ref,prepared_revision_ref,file_name,media_type,digest,size_bytes,
 scan_state,preview_state,object_key,object_version,object_etag,state,request_digest
FROM control_plane.prepared_content
WHERE organization_id=@organization_id AND actor_id=@actor_id AND intent_operation=@intent_operation
 AND idempotency_key=@idempotency_key AND operation_key=@operation_key AND available_until>clock_timestamp();
