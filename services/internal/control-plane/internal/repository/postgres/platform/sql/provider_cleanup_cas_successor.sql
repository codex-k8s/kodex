-- name: provider_cleanup_cas_successor :exec
INSERT INTO control_plane.provider_credential_cleanup_tasks
 (ref,organization_id,provider_account_id,target_kind,provider_authorization_attempt_id,
  materializer_attempt_ref,predecessor_task_id,eligible_at,maximum_attempts,attempts,lease_generation)
SELECT @next_ref,organization_id,provider_account_id,'AUTHORIZATION_METADATA',provider_authorization_attempt_id,
       materializer_attempt_ref,id,clock_timestamp(),maximum_attempts,attempts,lease_generation
FROM control_plane.provider_credential_cleanup_tasks
WHERE ref=@parent_ref AND state='CLAIMED'
  AND target_kind IN ('AUTHORIZATION_ATTEMPT','AUTHORIZATION_ABSENCE');
