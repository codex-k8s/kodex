-- name: runtime_claim__select_terminal_storage :many
SELECT node.id::text, node.ref, run.id::text, run.ref, run.root_run_id::text,
       COALESCE(run.project_id::text, ''), COALESCE(project.ref, ''),
       session.id::text, session.ref,
       CASE WHEN run.target_type = 'SYSTEM_ASSISTANT' AND agent.system_key = 'system-assistant'
                 AND agent.project_id IS NULL THEN 'system-assistant' ELSE '' END
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id = node.run_id AND run.organization_id = node.organization_id
JOIN control_plane.runs root ON root.id = run.root_run_id AND root.organization_id = run.organization_id
JOIN control_plane.sessions session ON session.id = run.session_id AND session.organization_id = run.organization_id
JOIN control_plane.session_storage storage ON storage.session_id = session.id AND storage.organization_id = run.organization_id
JOIN control_plane.agents agent ON agent.id = node.agent_id AND agent.organization_id = run.organization_id
LEFT JOIN control_plane.projects project ON project.id = run.project_id AND project.organization_id = run.organization_id
WHERE node.organization_id = @organization_id::uuid
  AND node.type = 'AGENT_EXECUTION' AND node.state = 'QUEUED'
  AND run.state IN ('QUEUED', 'RUNNING') AND root.state IN ('QUEUED', 'RUNNING')
  AND storage.state IN ('ERROR', 'PURGED')
ORDER BY node.created_at, node.id
FOR UPDATE OF node, storage SKIP LOCKED
LIMIT @limit;
