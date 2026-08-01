-- name: ProcessHasActiveChildren
SELECT EXISTS (
    SELECT 1
    FROM control_plane.resources AS child
    WHERE child.organization_id = @organization_id::uuid
      AND child.project_id = @project_id::uuid
      AND child.kind = 'PROCESS_RUN'
      AND child.spec ->> 'parentProcessRunId' = @process_run_id
      AND child.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED')
)
