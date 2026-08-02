SELECT id, organization_id, project_id, process_id, session_id, thread_id,
       role_id, turn_id, attempt, runtime_revision_id,
       runtime_revision_version, runtime_revision_sha256,
       immutable_input_sha256, resource_class, cluster_access_profile,
       workload_id, workload_spiffe_id, grant_generation, version, fence,
       state, coalesce(lease_id::text, ''), coalesce(lease_token_sha256, ''),
       coalesce(lease_expires_at, 'epoch'::timestamptz),
       coalesce(terminal_outcome, ''), coalesce(terminal_reference, ''),
       coalesce(terminal_sha256, ''), coalesce(archive_reference, ''),
       coalesce(archive_sha256, ''), coalesce(restore_proof_reference, ''),
       coalesce(restore_proof_sha256, ''),
       coalesce(restore_verifier_workload_id, ''),
       coalesce(cleanup_authorization_id::text, ''),
       coalesce(cleanup_authorization_expires_at, 'epoch'::timestamptz),
       created_at, updated_at
FROM control_plane.runtime_executions
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND turn_id = @turn_id
  AND attempt = @attempt
  AND state IN ('ADMITTED', 'RUNNING')
  AND lease_expires_at <= clock_timestamp()
ORDER BY lease_expires_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED;
