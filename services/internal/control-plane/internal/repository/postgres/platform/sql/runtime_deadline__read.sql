-- name: runtime_deadline__read :many
WITH RECURSIVE lineage AS (
    SELECT id,parent_run_id,root_run_id,organization_id,project_id,0 AS depth
    FROM control_plane.runs WHERE id=@run_id::uuid AND organization_id=@organization_id::uuid
    UNION ALL
    SELECT parent.id,parent.parent_run_id,parent.root_run_id,parent.organization_id,parent.project_id,child.depth+1
    FROM lineage child JOIN control_plane.runs parent ON parent.id=child.parent_run_id
      AND parent.organization_id=child.organization_id AND parent.project_id IS NOT DISTINCT FROM child.project_id
    WHERE child.depth<128
), owners AS (SELECT id FROM lineage UNION SELECT root_run_id FROM lineage)
SELECT run.ref,version.ref,version.digest,run.execution_step_key,run.execution_timeout_seconds,
       run.execution_started_at,run.execution_deadline_at,run.execution_deadline_at<=clock_timestamp()
FROM owners JOIN control_plane.runs run ON run.id=owners.id AND run.organization_id=@organization_id::uuid
JOIN control_plane.workflow_versions version ON version.id=run.workflow_version_id
WHERE run.execution_started_at IS NOT NULL
ORDER BY run.ref;
