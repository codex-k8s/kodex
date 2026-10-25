-- name: secret_retention_claim_readback :one
SELECT run.state, run.version,
       (SELECT array_agg(node.state ORDER BY node.ref) FROM control_plane.run_nodes node WHERE node.run_id=run.id),
       (SELECT count(*) FROM control_plane.runtime_revisions revision WHERE revision.run_id=run.id),
       (SELECT count(*) FROM control_plane.runtime_leases lease WHERE lease.run_id=run.id),
       (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=run.id)
FROM control_plane.runs run WHERE run.ref=$1;
