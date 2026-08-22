-- name: platform__workers_claimintegrationtests_claim_test_lease :exec
UPDATE control_plane.integration_connection_tests SET state='CLAIMED',lease_ref=$2,fence_digest=$3,generation=$4,workload_instance=$5,lease_expires_at=$6,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND state='DUE'
