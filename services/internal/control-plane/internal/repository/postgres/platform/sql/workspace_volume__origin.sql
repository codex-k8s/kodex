-- name: workspace_volume__origin :one
SELECT root.initiated_by::text,root.ref,p.ref,s.ref,a.ref,root.target_type,root.target_ref
FROM control_plane.run_nodes n
JOIN control_plane.runs r ON r.id=n.run_id AND r.organization_id=n.organization_id
JOIN control_plane.runs root ON root.id=r.root_run_id AND root.organization_id=r.organization_id
JOIN control_plane.projects p ON p.id=r.project_id AND p.organization_id=r.organization_id AND p.lifecycle='ACTIVE'
JOIN control_plane.sessions s ON s.id=r.session_id AND s.organization_id=r.organization_id AND s.project_id=r.project_id AND s.state='ACTIVE'
JOIN control_plane.agents a ON a.id=n.agent_id AND a.organization_id=r.organization_id AND a.project_id=r.project_id
 AND s.target_type='AGENT' AND s.target_ref=a.ref AND a.enabled
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
WHERE n.organization_id=@organization_id::uuid AND n.id=@node_id::uuid AND root.id=@root_run_id::uuid
 AND n.type='AGENT_EXECUTION' AND n.state='QUEUED' AND r.state IN('QUEUED','RUNNING')
 AND root.state IN('QUEUED','RUNNING','WAITING_HUMAN');
