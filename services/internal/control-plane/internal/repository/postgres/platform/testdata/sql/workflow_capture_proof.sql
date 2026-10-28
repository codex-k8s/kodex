-- name: workflow_capture_proof :one
SELECT node.workflow_captured_artifacts,node.workflow_business_attempt,lease.state,
       revision.id::text,lease.id::text,node.turn_id::text,node.attempt,lease.generation,
       (SELECT count(*) FROM control_plane.artifacts artifact WHERE artifact.node_id=node.id)
FROM control_plane.run_nodes node
JOIN control_plane.runtime_leases lease ON lease.node_id=node.id
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
WHERE node.ref=@node_ref AND lease.ref=@lease_ref;
