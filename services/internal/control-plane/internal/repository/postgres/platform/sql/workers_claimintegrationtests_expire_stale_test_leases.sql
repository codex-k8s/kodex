-- name: platform__workers_claimintegrationtests_expire_stale_test_leases :exec
UPDATE control_plane.integration_connection_tests SET state='DUE',lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,attempt=attempt+1,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND state='CLAIMED' AND lease_expires_at<=clock_timestamp()
