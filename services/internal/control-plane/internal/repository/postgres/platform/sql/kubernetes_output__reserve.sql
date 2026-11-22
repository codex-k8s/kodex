-- name: kubernetes_output__reserve :one
INSERT INTO control_plane.kubernetes_output_stages
(ref,organization_id,stream_id,sequence,channel,payload_digest,byte_count,delivery_ordinal,state,authorized_until,object_key)
VALUES(@ref,@organization_id::uuid,@stream_id::uuid,@sequence,@channel,@payload_digest,@byte_count,
 @delivery_ordinal,'ATTEMPTING',@authorized_until,@object_key)
RETURNING id::text;
