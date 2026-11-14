-- name: telegram_claim_deliveries :many
WITH expired AS (
 UPDATE control_plane.interaction_deliveries delivery
 SET state=CASE WHEN telegram_effect_started_at IS NOT NULL THEN 'UNKNOWN_OUTCOME'
   WHEN attempt>=execution_max_attempts OR telegram_expires_at<=clock_timestamp() THEN 'CANCELLED' ELSE 'FAILED' END,
  safe_error_code=CASE WHEN telegram_effect_started_at IS NOT NULL THEN 'TELEGRAM_OUTCOME_UNKNOWN' ELSE 'TELEGRAM_LEASE_EXPIRED' END,
  lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,available_at=clock_timestamp()+interval '1 second',
  completed_at=CASE WHEN telegram_effect_started_at IS NOT NULL OR attempt>=execution_max_attempts OR telegram_expires_at<=clock_timestamp()
   THEN clock_timestamp() ELSE NULL END,version=version+1,updated_at=clock_timestamp()
 WHERE organization_id=@organization_id::uuid AND capability_key='telegram.notifications'
  AND state='CLAIMED' AND lease_expires_at<=clock_timestamp()
 RETURNING id
), stale AS (
 UPDATE control_plane.interaction_deliveries delivery
 SET state=CASE WHEN telegram_effect_started_at IS NULL THEN 'CANCELLED' ELSE 'UNKNOWN_OUTCOME' END,
  safe_error_code=CASE WHEN telegram_effect_started_at IS NULL THEN 'TELEGRAM_SCOPE_REVOKED' ELSE 'TELEGRAM_OUTCOME_UNKNOWN' END,
  lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
  completed_at=clock_timestamp(),version=delivery.version+1,updated_at=clock_timestamp()
 FROM control_plane.telegram_subscriptions subscription
 WHERE delivery.organization_id=@organization_id::uuid AND delivery.telegram_subscription_id=subscription.id
  AND delivery.state IN ('DUE','FAILED') AND (delivery.telegram_expires_at<=clock_timestamp()
   OR subscription.version<>delivery.telegram_subscription_version OR subscription.scope_digest<>delivery.telegram_scope_digest
   OR NOT control_plane.telegram_subscription_eligible(subscription.id,delivery.root_run_id,delivery.telegram_gate_id,delivery.telegram_input->>'event_type'))
 RETURNING delivery.id
), candidates AS (
 SELECT delivery.id,gen_random_uuid()::text AS fence
 FROM control_plane.interaction_deliveries delivery
 JOIN control_plane.telegram_subscriptions subscription ON subscription.id=delivery.telegram_subscription_id
 WHERE delivery.organization_id=@organization_id::uuid AND delivery.capability_key='telegram.notifications'
  AND delivery.state IN ('DUE','FAILED') AND delivery.attempt<delivery.execution_max_attempts
  AND delivery.available_at<=clock_timestamp() AND delivery.telegram_expires_at>clock_timestamp()
  AND delivery.telegram_effect_started_at IS NULL AND subscription.version=delivery.telegram_subscription_version
  AND subscription.scope_digest=delivery.telegram_scope_digest AND subscription.credential_revision_id=delivery.telegram_credential_revision_id
  AND control_plane.telegram_subscription_eligible(subscription.id,delivery.root_run_id,delivery.telegram_gate_id,delivery.telegram_input->>'event_type')
  AND (SELECT count(*) FROM expired)>=0 AND (SELECT count(*) FROM stale)>=0
 ORDER BY delivery.available_at,delivery.created_at
 FOR UPDATE OF subscription,delivery SKIP LOCKED LIMIT @claim_limit
), claimed AS (
 UPDATE control_plane.interaction_deliveries delivery
 SET state='CLAIMED',attempt=attempt+1,generation=generation+1,
  lease_ref='idl_'||replace(gen_random_uuid()::text,'-',''),fence_digest=encode(digest(candidate.fence,'sha256'),'hex'),
  workload_instance=@workload_instance,lease_expires_at=clock_timestamp()+interval '45 seconds',safe_error_code='',
  version=version+1,updated_at=clock_timestamp()
 FROM candidates candidate WHERE delivery.id=candidate.id
 RETURNING delivery.*,candidate.fence
)
SELECT claimed.ref,connection.ref,claimed.telegram_input,claimed.telegram_input_digest,claimed.lease_ref,claimed.fence,
 claimed.generation,claimed.lease_expires_at,project.language,
 credential.ref,credential.revision,credential.secret_ref,credential.secret_uid::text,
 credential.secret_resource_version,credential.content_sha256,credential.created_at
FROM claimed
JOIN control_plane.integration_connections connection ON connection.id=claimed.connection_id
JOIN control_plane.projects project ON project.id=claimed.project_id
JOIN control_plane.integration_credential_revisions credential ON credential.id=claimed.telegram_credential_revision_id
 AND credential.organization_id=claimed.organization_id AND credential.connection_id=connection.id
ORDER BY claimed.created_at;
