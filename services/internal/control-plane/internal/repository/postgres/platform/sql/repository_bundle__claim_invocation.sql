-- name: repository_bundle__claim_invocation :exec
UPDATE control_plane.integration_invocations
SET state='RUNNING',lease_ref=@lease_ref,effect_fence_digest=@fence_digest,generation=@generation,
    workload_instance=@claimant,lease_expires_at=@expires_at,claimed_workload='integration-gateway',
    claimed_lease_ref=@lease_ref,claimed_fence_digest=@fence_digest,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@invocation_id::uuid
  AND operation='github.repository.bundle.publish' AND state IN ('READY','UNKNOWN_OUTCOME','RUNNING')
  AND generation<@generation AND effect_receipt_id IS NULL;
