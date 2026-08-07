-- name: RunGraphNodes
WITH RECURSIVE ancestors AS (
    SELECT resource.id, resource.spec ->> 'parentProcessRunId' AS parent_id
    FROM control_plane.resources AS resource
    WHERE resource.organization_id = @organization_id::uuid
      AND resource.project_id = @project_id::uuid
      AND resource.owner_actor_id = @actor_id::uuid
      AND resource.id = @process_run_id::uuid
      AND resource.kind = 'PROCESS_RUN' AND resource.state <> 'DELETED'
    UNION ALL
    SELECT parent.id, parent.spec ->> 'parentProcessRunId'
    FROM control_plane.resources AS parent
    JOIN ancestors AS child ON parent.id = nullif(child.parent_id, '')::uuid
    WHERE parent.organization_id = @organization_id::uuid
      AND parent.project_id = @project_id::uuid
      AND parent.owner_actor_id = @actor_id::uuid
      AND parent.kind = 'PROCESS_RUN' AND parent.state <> 'DELETED'
), root AS (
    SELECT id FROM ancestors WHERE coalesce(parent_id, '') = '' LIMIT 1
), processes AS (
    SELECT resource.*
    FROM control_plane.resources AS resource JOIN root ON root.id = resource.id
    UNION ALL
    SELECT child.*
    FROM control_plane.resources AS child
    JOIN processes AS parent ON nullif(child.spec ->> 'parentProcessRunId', '')::uuid = parent.id
    WHERE child.organization_id = @organization_id::uuid
      AND child.project_id = @project_id::uuid
      AND child.owner_actor_id = @actor_id::uuid
      AND child.kind = 'PROCESS_RUN' AND child.state <> 'DELETED'
), executions AS (
    SELECT execution.* FROM control_plane.runtime_executions AS execution
    JOIN processes AS process ON process.id = execution.process_id
    WHERE execution.organization_id = @organization_id::uuid
      AND execution.project_id = @project_id::uuid
), graph_ids AS (
    SELECT id FROM processes
    UNION SELECT id FROM executions
    UNION SELECT session_id FROM executions
    UNION SELECT turn_id FROM executions
    UNION SELECT runtime_revision_id FROM executions
)
SELECT 'PROCESS' AS node_type, process.id::text, process.state, coalesce(process.spec ->> 'parentProcessRunId', ''),
       process.id::text, '' AS session_id, '' AS turn_id, '' AS runtime_revision_id,
       process.version, 0::bigint AS runtime_revision_version, 0::integer AS attempt,
       process.created_at, process.updated_at
FROM processes AS process
UNION ALL
SELECT 'ATTEMPT', execution.id::text, execution.state, '', execution.process_id::text,
       execution.session_id::text, execution.turn_id::text, execution.runtime_revision_id::text,
       execution.version, execution.runtime_revision_version, execution.attempt,
       execution.created_at, execution.updated_at
FROM executions AS execution
UNION ALL
SELECT 'ARTIFACT', artifact.id::text, artifact.state, '', '' AS process_run_id,
       '' AS session_id, '' AS turn_id, '' AS runtime_revision_id,
       artifact.version, 0::bigint, 0::integer, artifact.created_at, artifact.updated_at
FROM control_plane.resources AS artifact
JOIN graph_ids ON graph_ids.id = artifact.parent_id
WHERE artifact.organization_id = @organization_id::uuid
  AND artifact.project_id = @project_id::uuid
  AND artifact.owner_actor_id = @actor_id::uuid
  AND artifact.kind = 'ARTIFACT' AND artifact.state <> 'DELETED'
ORDER BY 12, 2;
