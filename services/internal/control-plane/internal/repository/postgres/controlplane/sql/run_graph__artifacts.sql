-- name: RunGraphArtifacts
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
    SELECT resource.* FROM control_plane.resources AS resource JOIN root ON root.id = resource.id
    UNION ALL
    SELECT child.* FROM control_plane.resources AS child
    JOIN processes AS parent ON nullif(child.spec ->> 'parentProcessRunId', '')::uuid = parent.id
    WHERE child.organization_id = @organization_id::uuid
      AND child.project_id = @project_id::uuid
      AND child.owner_actor_id = @actor_id::uuid
      AND child.kind = 'PROCESS_RUN' AND child.state <> 'DELETED'
), turns AS (
    SELECT turn.*
    FROM control_plane.resources AS turn
    JOIN processes AS process ON process.id = nullif(turn.spec ->> 'processRunId', '')::uuid
    WHERE turn.organization_id = @organization_id::uuid
      AND turn.project_id = @project_id::uuid
      AND turn.owner_actor_id = @actor_id::uuid
      AND turn.kind = 'TURN' AND turn.state <> 'DELETED'
), graph_ids AS (
    SELECT id FROM processes
    UNION SELECT id FROM turns
    UNION SELECT nullif(turn.spec ->> 'sessionId', '')::uuid FROM turns AS turn
    UNION SELECT nullif(turn.spec ->> 'runtimeRevisionId', '')::uuid FROM turns AS turn
    UNION SELECT attempt.runtime_revision_id
        FROM control_plane.turn_attempts AS attempt JOIN turns ON turns.id = attempt.turn_id
    UNION SELECT execution.id FROM control_plane.runtime_executions AS execution
        JOIN turns ON turns.id = execution.turn_id
), artifact_ids AS (
    SELECT artifact.id
    FROM control_plane.resources AS artifact
    JOIN graph_ids ON graph_ids.id = artifact.parent_id
    WHERE artifact.kind = 'ARTIFACT'
    UNION
    SELECT reference.id
    FROM turns AS turn
    CROSS JOIN LATERAL (
        SELECT nullif(turn.spec ->> 'promptArtifactId', '')::uuid AS id
        UNION SELECT nullif(turn.spec ->> 'resultArtifactId', '')::uuid
        UNION SELECT nullif(input.value ->> 'artifactId', '')::uuid
              FROM jsonb_array_elements(coalesce(turn.spec -> 'inputArtifacts', '[]'::jsonb)) AS input(value)
    ) AS reference
    WHERE reference.id IS NOT NULL
)
SELECT artifact.id::text, artifact.organization_id::text, artifact.project_id::text,
       coalesce(artifact.parent_id::text, ''), artifact.owner_actor_id::text,
       artifact.kind, artifact.name, artifact.state, artifact.version, artifact.spec,
       artifact.created_at, artifact.updated_at
FROM control_plane.resources AS artifact
JOIN artifact_ids ON artifact_ids.id = artifact.id
WHERE artifact.organization_id = @organization_id::uuid
  AND artifact.project_id = @project_id::uuid
  AND artifact.owner_actor_id = @actor_id::uuid
  AND artifact.kind = 'ARTIFACT' AND artifact.state <> 'DELETED'
  AND (@after_occurred_at::timestamptz IS NULL OR (artifact.created_at, artifact.id) >
      (@after_occurred_at::timestamptz, nullif(@after_id, '')::uuid))
ORDER BY artifact.created_at, artifact.id
LIMIT @limit;
