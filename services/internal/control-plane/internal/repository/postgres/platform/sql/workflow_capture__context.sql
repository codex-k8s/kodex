-- name: workflow_capture__context :one
SELECT COALESCE(version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND EXISTS (
           SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step
           WHERE step->>'Key'=node.workflow_step_key),false),
       node.workflow_business_attempt
FROM control_plane.run_nodes node
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
LEFT JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id
WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
    AND (root.workflow_version_id IS NULL OR (
        version.organization_id=root.organization_id AND EXISTS (
            SELECT 1 FROM control_plane.workflows workflow WHERE workflow.id=version.workflow_id
                AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id)));
