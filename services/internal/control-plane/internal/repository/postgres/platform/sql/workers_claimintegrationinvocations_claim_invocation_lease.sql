-- name: platform__workers_claimintegrationinvocations_claim_invocation_lease :exec
UPDATE control_plane.integration_invocations SET state='RUNNING',lease_ref=$2,effect_fence_digest=$3,generation=$4,workload_instance=$5,lease_expires_at=$6,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND state='READY'
