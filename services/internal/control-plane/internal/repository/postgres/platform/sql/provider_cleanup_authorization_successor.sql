-- name: provider_cleanup_authorization_successor :exec
INSERT INTO control_plane.provider_credential_cleanup_tasks
    (ref, organization_id, provider_account_id, target_kind,
     provider_authorization_attempt_id, materializer_attempt_ref,
     materializer_attempt_uid, materializer_attempt_resource_version,
     predecessor_task_id, eligible_at, maximum_attempts, lease_generation, attempts)
SELECT @next_ref, parent.organization_id, parent.provider_account_id, @target_kind,
       parent.provider_authorization_attempt_id, parent.materializer_attempt_ref,
       NULLIF(@object_uid, '')::uuid, NULLIF(@object_version, ''), parent.id,
       clock_timestamp(), parent.maximum_attempts, parent.lease_generation, GREATEST(parent.attempts-1,0)
FROM control_plane.provider_credential_cleanup_tasks parent
WHERE parent.ref = @parent_ref
  AND parent.target_kind = 'AUTHORIZATION_METADATA'
  AND parent.state = 'CLAIMED';
