-- name: platform__queries_getoverview_1 :one
SELECT
		(SELECT count(*) FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.lifecycle='ACTIVE'),
		(SELECT count(*) FROM control_plane.agents a WHERE a.organization_id=$1::uuid AND a.system_key IS NULL AND a.state<>'ARCHIVED'),
		(SELECT count(*) FROM control_plane.runs r WHERE r.organization_id=$1::uuid AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
		(SELECT count(*) FROM control_plane.owner_gates g WHERE g.organization_id=$1::uuid AND g.state='OPEN')
