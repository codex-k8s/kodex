-- name: workflow_dispatch__active :one
SELECT count(*) FILTER (WHERE node.state IN ('QUEUED','RUNNING','WAITING')),
       count(*) FILTER (WHERE node.state='RUNNING')
FROM control_plane.run_nodes node
WHERE node.organization_id=@organization_id::uuid AND node.root_run_id=@root_run_id::uuid
    AND node.type='AGENT_EXECUTION';
