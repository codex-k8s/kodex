-- name: kubernetes_input__stage :one
SELECT stage.id::text,stage.ref,stage.sequence,stage.channel,stage.payload_digest,stage.byte_count,stage.end_of_input,
 stage.state,coalesce(stage.object_key,''),coalesce(stage.object_version,''),coalesce(stage.object_etag,''),stage.delivery_expires_at,stage.native_admitted_at,
 stage.submission_key,stage.intent_digest
FROM control_plane.kubernetes_input_stages stage WHERE stage.stream_id=@stream_id::uuid AND stage.organization_id=@organization_id::uuid
 AND (@submission_key='' OR stage.submission_key=@submission_key AND stage.actor_id=nullif(@actor_id,'')::uuid)
ORDER BY stage.sequence DESC LIMIT 1 FOR UPDATE;
