-- name: runtime_completeexecution_fail_root_run :exec
UPDATE control_plane.runs SET state='FAILED',result_summary=$2,safe_error_code=$3,safe_error_message=$4,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
