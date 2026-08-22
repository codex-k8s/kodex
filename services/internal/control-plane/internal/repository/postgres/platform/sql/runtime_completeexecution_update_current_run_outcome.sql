-- name: platform__runtime_completeexecution_update_current_run_outcome :exec
UPDATE control_plane.runs SET state=$2,result_summary=$3,safe_error_code=$4,safe_error_message=$5,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND id<>root_run_id
