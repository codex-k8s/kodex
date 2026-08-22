-- name: platform__commands_changerun_5 :exec
UPDATE control_plane.owner_gates SET state='CANCELLED',decision='CANCEL',decision_comment='Запуск отменён',resolved_by=$2::uuid,resolved_at=clock_timestamp(),version=version+1 WHERE root_run_id=$1::uuid AND state='OPEN'
