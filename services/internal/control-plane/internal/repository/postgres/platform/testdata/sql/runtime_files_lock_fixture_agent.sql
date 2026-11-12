-- name: runtime_files_lock_fixture_agent :one
SELECT agent.id::text,agent.capabilities FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.agents agent ON agent.id=revision.agent_id AND agent.organization_id=lease.organization_id
WHERE lease.ref=$1 FOR UPDATE OF agent;
