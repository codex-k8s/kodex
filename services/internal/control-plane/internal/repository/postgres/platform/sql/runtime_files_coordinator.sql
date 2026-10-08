-- name: runtime_files_coordinator :one
SELECT control_plane.runtime_file_coordinator(run.organization_id,root.initiated_by,run.project_id,node.agent_id,node.id)
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.run_id=run.id AND node.organization_id=run.organization_id
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref AND node.ref=@node_ref;
