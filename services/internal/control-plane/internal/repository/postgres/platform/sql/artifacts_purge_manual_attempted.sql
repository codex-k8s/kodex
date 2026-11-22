-- name: artifacts_purge_manual_attempted :one
SELECT coalesce(convert_from(response_payload,'UTF8')::jsonb->'deleteAttempts','{}'::jsonb)?@revision_id
FROM control_plane.idempotency_receipts
WHERE organization_id=@organization_id::uuid AND actor_id=@actor_id::uuid AND operation='artifact.purge'
 AND idempotency_key=@idempotency_key AND intent_digest=@intent_digest
 AND convert_from(response_payload,'UTF8')::jsonb->>'artifactId'=@artifact_id;
