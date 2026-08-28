-- name: workers_completeintegrationinvocation_update_integration_invocations_state_result_summary_safe_error_code :exec
UPDATE control_plane.integration_invocations
SET state=$2,result_summary=$3,safe_error_code=$4,effect_receipt_id=$5::uuid,
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid
