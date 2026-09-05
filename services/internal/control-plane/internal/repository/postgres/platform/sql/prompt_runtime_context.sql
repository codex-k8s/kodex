-- name: prompt_runtime_context :one
SELECT COALESCE(project.language, 'en'), agent.version,
       CASE WHEN root.target_type = 'WORKFLOW' THEN root.target_ref ELSE '' END,
       COALESCE(version.ref, ''), COALESCE(version.version_number, 0), version.spec
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id = node.run_id
JOIN control_plane.runs root ON root.id = run.root_run_id
JOIN control_plane.agents agent ON agent.id = node.agent_id
LEFT JOIN control_plane.projects project ON project.id = run.project_id
LEFT JOIN control_plane.workflow_versions version ON version.id = root.workflow_version_id
WHERE node.organization_id = @organization_id::uuid AND node.ref = @node_ref
  AND agent.organization_id = node.organization_id;
