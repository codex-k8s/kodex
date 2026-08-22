-- name: platform__runtime_completeexecution_update_runs_state_result_summary_finished_at :exec
UPDATE control_plane.runs SET state='SUCCEEDED',result_summary=$2,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
