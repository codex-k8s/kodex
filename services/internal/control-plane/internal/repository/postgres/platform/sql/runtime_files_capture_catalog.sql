-- name: runtime_files_capture_catalog :one
INSERT INTO control_plane.runtime_file_catalogs(ref,organization_id,project_id,actor_id,agent_id,node_id,
    run_id,session_id,turn_id,runtime_revision_ref,generation,purposes)
SELECT @catalog_ref,run.organization_id,run.project_id,root.initiated_by,node.agent_id,node.id,
    run.id,run.session_id,node.turn_id,@revision_ref,@generation,@purposes
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.run_id=run.id AND node.organization_id=run.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=run.organization_id
  AND (agent.project_id=run.project_id OR (agent.project_id IS NULL AND agent.system_key='system-assistant'))
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref AND node.ref=@node_ref
RETURNING id::text;
