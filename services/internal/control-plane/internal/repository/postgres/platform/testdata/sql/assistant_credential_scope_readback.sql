SELECT root_run.initiated_by::text,revision.organization_id::text,
 COALESCE(agent.project_id::text,''),COALESCE(revision.project_id::text,''),
 revision.safe_snapshot->>'assistantScope'
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.runs root_run ON root_run.id=revision.root_run_id
JOIN control_plane.agents agent ON agent.id=revision.agent_id
WHERE lease.ref=$1;
