WITH root AS (
    SELECT root_run_id FROM control_plane.runs WHERE ref = $1
)
SELECT
    (SELECT count(*) FROM root),
    (SELECT count(*) FROM control_plane.run_nodes node JOIN root ON root.root_run_id = node.root_run_id
     WHERE node.state IN ('QUEUED','RUNNING','WAITING','CONTINUATION')),
    (SELECT count(*) FROM control_plane.runtime_leases lease JOIN control_plane.run_nodes node ON node.id = lease.node_id
     JOIN root ON root.root_run_id = node.root_run_id WHERE lease.state = 'CLAIMED'),
    (SELECT count(*) FROM control_plane.session_turns turn JOIN control_plane.runs run ON run.id = turn.run_id
     JOIN root ON root.root_run_id = run.root_run_id WHERE turn.state IN ('QUEUED','RUNNING','WAITING'));
