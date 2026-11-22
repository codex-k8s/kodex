-- name: kubernetes_input__reserve :one
INSERT INTO control_plane.kubernetes_input_stages(ref,organization_id,stream_id,actor_id,submission_key,intent_digest,
 sequence,channel,payload_digest,byte_count,end_of_input,state,object_key)
VALUES(@ref,@organization_id::uuid,@stream_id::uuid,@actor_id::uuid,@submission_key,@intent_digest,
 @sequence,@channel,@payload_digest,@byte_count,@end_of_input,'ATTEMPTING',nullif(@object_key,'')) RETURNING id::text;
