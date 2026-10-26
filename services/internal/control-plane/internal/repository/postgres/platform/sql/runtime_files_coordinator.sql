-- name: runtime_files_coordinator :one
SELECT CASE WHEN version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    THEN control_plane.runtime_workflow_step(run.organization_id,node.id)
    ELSE control_plane.runtime_file_coordinator(run.organization_id,root.initiated_by,run.project_id,node.agent_id,node.id) END
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.run_id=run.id AND node.organization_id=run.organization_id
LEFT JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref AND node.ref=@node_ref;
