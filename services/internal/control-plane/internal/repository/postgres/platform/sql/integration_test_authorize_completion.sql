-- name: integration_test_authorize_completion :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.integration_connection_tests
    WHERE organization_id=$1::uuid AND ref=$2 AND claimed_workload=$3
      AND claimed_lease_ref=$4 AND claimed_fence_digest=$5 AND generation=$6
      AND claimed_lease_ref<>'' AND claimed_fence_digest<>''
);
