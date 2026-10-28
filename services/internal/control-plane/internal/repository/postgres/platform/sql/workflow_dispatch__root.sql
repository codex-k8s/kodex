-- name: workflow_dispatch__root :one
SELECT root.ref,root.state,root.version,root.project_id::text,project.ref,root.initiated_by::text,
       root.task,root.input,version.ref,version.digest,version.spec,
       COALESCE(root.workflow_exception,'null'::jsonb)
FROM control_plane.runs root
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
    AND workflow.project_id=root.project_id
WHERE root.id=@root_run_id::uuid AND root.organization_id=@organization_id::uuid AND root.root_run_id=root.id
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
FOR UPDATE OF root;
