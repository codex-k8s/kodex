-- name: workers_completeintegrationtest_update_integration_connection_tests_state_result_summary_safe_error_code :exec
UPDATE control_plane.integration_connection_tests SET state=$2,result_summary=$3,safe_error_code=$4,lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,completed_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
