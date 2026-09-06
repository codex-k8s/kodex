-- name: runtime_claimexecution_expire_stale_leases :many
WITH expired AS (
		UPDATE control_plane.runtime_leases
		SET state='EXPIRED',updated_at=clock_timestamp()
		WHERE organization_id=$1::uuid AND state='CLAIMED' AND expires_at<=clock_timestamp()
		RETURNING node_id, run_id, ref
	), requeued AS (UPDATE control_plane.run_nodes n
	SET state='QUEUED',started_at=NULL,progress_summary='',version=n.version+1
	FROM expired e,control_plane.runs r
	WHERE n.id=e.node_id AND r.id=n.run_id AND r.state IN('QUEUED','RUNNING')
	RETURNING n.id)
SELECT expired.ref, COALESCE(run.project_id::text, ''), run.ref
FROM expired JOIN control_plane.runs run ON run.id=expired.run_id
ORDER BY expired.ref;
