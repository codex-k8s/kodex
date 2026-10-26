-- name: workflow_journal_context :one
SELECT node.organization_id::text,node.id::text,node.root_run_id::text,revision.id::text,lease.id::text,
       node.workflow_attempts
FROM control_plane.run_nodes node
JOIN control_plane.runtime_leases lease ON lease.node_id=node.id AND lease.organization_id=node.organization_id
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
WHERE node.ref=@node_ref AND lease.ref=@lease_ref;
