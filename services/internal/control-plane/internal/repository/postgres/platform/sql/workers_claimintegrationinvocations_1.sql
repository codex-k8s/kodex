-- name: platform__workers_claimintegrationinvocations_1 :exec
UPDATE control_plane.integration_invocations SET state='READY',lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND state='RUNNING' AND lease_expires_at<=clock_timestamp()
