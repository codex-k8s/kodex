-- name: platform__runtime_claimexecution_1 :exec
WITH expired AS (
		UPDATE control_plane.runtime_leases
		SET state='EXPIRED',updated_at=clock_timestamp()
		WHERE organization_id=$1::uuid AND state='CLAIMED' AND expires_at<=clock_timestamp()
		RETURNING node_id
	) UPDATE control_plane.run_nodes n
	SET state='QUEUED',started_at=NULL,progress_summary='',version=version+1
	FROM expired e,control_plane.runs r
	WHERE n.id=e.node_id AND r.id=n.run_id AND r.state IN('QUEUED','RUNNING')
