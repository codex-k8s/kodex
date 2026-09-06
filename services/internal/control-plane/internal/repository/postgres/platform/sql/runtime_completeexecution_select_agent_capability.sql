-- name: runtime_completeexecution_select_agent_capability :one
SELECT $2::text=ANY(agent.capabilities)
       AND COALESCE(revision.safe_snapshot->'capabilities','[]'::jsonb) ? $2::text,
       actor.ref
FROM control_plane.run_nodes node
JOIN control_plane.agents agent
  ON agent.id=node.agent_id
 AND agent.organization_id=node.organization_id
JOIN control_plane.runtime_leases lease ON lease.node_id=node.id
 AND lease.organization_id=node.organization_id AND lease.id=$4::uuid
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
 AND revision.organization_id=lease.organization_id
JOIN control_plane.runs run ON run.id=node.run_id
JOIN control_plane.runs root_run ON root_run.id=run.root_run_id
 AND root_run.organization_id=node.organization_id
JOIN control_plane.subjects actor ON actor.id=root_run.initiated_by
 AND actor.organization_id=node.organization_id AND actor.active
WHERE node.organization_id=$1::uuid
  AND node.id=$3::uuid
