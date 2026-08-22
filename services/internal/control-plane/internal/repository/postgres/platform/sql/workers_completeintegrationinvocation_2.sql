-- name: platform__workers_completeintegrationinvocation_2 :exec
UPDATE control_plane.integration_invocations SET state=$2,result_summary=$3,safe_error_code=$4,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
