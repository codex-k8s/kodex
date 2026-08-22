-- name: platform__commands_changerun_2 :exec
UPDATE control_plane.runs SET state='CANCELLED',safe_error_code='CANCELLED_BY_OWNER',safe_error_message='',finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE root_run_id=$1::uuid AND state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
