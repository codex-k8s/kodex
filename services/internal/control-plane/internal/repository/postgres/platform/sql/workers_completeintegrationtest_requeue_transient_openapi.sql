-- name: workers_completeintegrationtest_requeue_transient_openapi :one
UPDATE control_plane.integration_connection_tests
SET state='DUE',attempt=attempt+1,lease_ref=NULL,fence_digest=NULL,
    workload_instance=NULL,lease_expires_at=NULL,version=version+1,
    updated_at=clock_timestamp()
WHERE id=$1::uuid AND state='CLAIMED' AND lease_ref=$2 AND generation=$3
RETURNING ref
