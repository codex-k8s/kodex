-- name: workspace_archive__fail :one
UPDATE control_plane.session_archive_tasks
SET state=CASE WHEN attempt>=5 THEN 'DEAD_LETTER' ELSE 'READY' END,
 available_at=clock_timestamp()+LEAST(300,5*power(2,attempt))*interval '1 second',safe_error_code=@safe_error_code,
 workload_instance=NULL,lease_ref=NULL,fence_digest=NULL,lease_expires_at=NULL,
 completed_at=CASE WHEN attempt>=5 THEN clock_timestamp() ELSE NULL END,updated_at=clock_timestamp()
WHERE id=@task_id::uuid AND state='CLAIMED' AND generation=@generation AND lease_ref=@lease_ref AND fence_digest=@fence_digest
 AND lease_expires_at>clock_timestamp()
RETURNING state,state='READY';
