-- name: provider_account_deletion_retry :exec
WITH eligible AS MATERIALIZED (
 SELECT task.* FROM control_plane.provider_credential_cleanup_tasks task
 JOIN control_plane.provider_account_deletion_intents intent ON intent.provider_account_id=task.provider_account_id
  AND intent.organization_id=task.organization_id
 WHERE task.organization_id=@organization_id::uuid AND task.provider_account_id=@account_id::uuid
  AND intent.state='FAILED' AND task.state='DEAD_LETTER'
  AND task.target_kind IN ('CREDENTIAL','AUTHORIZATION_METADATA','AUTHORIZATION_ATTEMPT','AUTHORIZATION_ABSENCE')
  AND task.recovery_task_ref IS NOT NULL AND task.recovery_generation>0
  AND NOT EXISTS (SELECT 1 FROM control_plane.provider_credential_cleanup_tasks successor WHERE successor.predecessor_task_id=task.id)
 ORDER BY task.id FOR UPDATE OF task
), successors AS (
 INSERT INTO control_plane.provider_credential_cleanup_tasks
 (ref,organization_id,provider_account_id,provider_credential_revision_id,target_kind,
  secret_name,secret_uid,secret_resource_version,content_sha256,
  provider_authorization_attempt_id,materializer_attempt_ref,materializer_attempt_uid,materializer_attempt_resource_version,
  predecessor_task_id,eligible_at,maximum_attempts,lease_generation,
  recovery_task_ref,recovery_generation,recovery_legacy_last_generation)
 SELECT 'pcct_'||gen_random_uuid()::text,organization_id,provider_account_id,provider_credential_revision_id,target_kind,
  secret_name,secret_uid,secret_resource_version,content_sha256,
  provider_authorization_attempt_id,materializer_attempt_ref,materializer_attempt_uid,materializer_attempt_resource_version,
  id,clock_timestamp(),maximum_attempts,lease_generation,
  recovery_task_ref,recovery_generation,recovery_legacy_last_generation FROM eligible
 RETURNING predecessor_task_id
)
UPDATE control_plane.provider_credential_cleanup_tasks task
 SET state='COMPLETED',terminal_receipt='superseded:'||task.terminal_receipt,updated_at=clock_timestamp()
 WHERE id IN (SELECT predecessor_task_id FROM successors);
