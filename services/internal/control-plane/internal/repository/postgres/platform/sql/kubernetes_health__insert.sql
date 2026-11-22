-- name: kubernetes_health__insert :one
INSERT INTO control_plane.kubernetes_identity_health_attempts
 (ref,organization_id,personal_test_id,identity_revision_id,registry_revision,target_ref,target_revision,
  snapshot_digest,input_digest,lease_ref,fence_digest,generation,workload_instance,expires_at)
SELECT @ref,test.organization_id,test.id,@identity_id::uuid,@registry_revision,@target_ref,@target_revision,
 @snapshot_digest,@input_digest,test.lease_ref,test.fence_digest,test.generation,test.workload_instance,test.lease_expires_at
FROM control_plane.integration_personal_credential_tests test
WHERE test.id=@test_id::uuid AND test.organization_id=@organization_id::uuid AND test.state='CLAIMED'
 AND test.lease_expires_at>clock_timestamp()
RETURNING id::text;
