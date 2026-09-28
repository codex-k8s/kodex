-- name: assistant_search_resolve_lease :one
SELECT actor.ref, actor.id::text
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision
  ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.run_nodes node
  ON node.id=lease.node_id AND node.state='RUNNING'
JOIN control_plane.runs run
  ON run.id=lease.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runs root
  ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
JOIN control_plane.agents agent
  ON agent.id=node.agent_id AND agent.organization_id=lease.organization_id
JOIN control_plane.subjects actor
  ON actor.id=root.initiated_by AND actor.organization_id=lease.organization_id
WHERE lease.organization_id=@organization_id::uuid
  AND lease.ref=@lease_ref
  AND lease.fence_digest=@fence_digest
  AND lease.generation=@generation
  AND lease.state='CLAIMED'
  AND lease.expires_at>clock_timestamp()
  AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
  AND root.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
  AND agent.system_key='system-assistant'
  AND agent.state<>'ARCHIVED'
  AND revision.safe_snapshot->>'stableKey'='system-assistant'
  AND actor.active AND actor.kind='USER'
FOR SHARE OF lease;
