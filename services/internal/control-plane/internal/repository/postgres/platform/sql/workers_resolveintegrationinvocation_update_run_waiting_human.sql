-- name: workers_resolveintegrationinvocation_update_run_waiting_human :exec
UPDATE control_plane.runs target
SET state='WAITING_HUMAN',version=version+1,updated_at=clock_timestamp()
WHERE root_run_id=$1::uuid AND state='RUNNING'
    AND (target.id=$1::uuid OR NOT EXISTS (
        SELECT 1 FROM control_plane.runs root
        JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
        JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
        WHERE root.id=$1::uuid AND root.root_run_id=root.id AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    ));
