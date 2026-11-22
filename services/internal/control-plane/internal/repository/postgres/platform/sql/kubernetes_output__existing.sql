-- name: kubernetes_output__existing :one
SELECT id::text,ref,channel,payload_digest,byte_count,delivery_ordinal,state,authorized_until,
 object_key,coalesce(object_version,''),coalesce(object_etag,'')
FROM control_plane.kubernetes_output_stages
WHERE stream_id=@stream_id::uuid AND organization_id=@organization_id::uuid AND sequence=@sequence
FOR UPDATE;
