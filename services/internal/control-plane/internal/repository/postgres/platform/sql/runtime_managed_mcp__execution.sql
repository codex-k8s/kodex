-- name: runtime_managed_mcp__execution :one
SELECT revision.safe_snapshot
FROM control_plane.runtime_revisions revision
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
  AND lease.organization_id=revision.organization_id AND lease.node_id=revision.node_id
JOIN control_plane.run_nodes node ON node.id=revision.node_id AND node.state='RUNNING'
JOIN control_plane.runs run ON run.id=node.run_id
JOIN control_plane.runs root ON root.id=run.root_run_id
WHERE revision.organization_id=@organization_id::uuid AND revision.node_id=@node_id::uuid
  AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
  AND root.state IN ('RUNNING','WAITING_HUMAN')
  AND revision.generation=(SELECT max(latest.generation)
                           FROM control_plane.runtime_revisions latest WHERE latest.node_id=revision.node_id)
FOR SHARE OF revision,lease;
