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
), turns AS (
    SELECT turn.*
    FROM control_plane.resources AS turn
    JOIN processes AS process ON process.id = nullif(turn.spec ->> 'processRunId', '')::uuid
    WHERE turn.organization_id = @organization_id::uuid
      AND turn.project_id = @project_id::uuid
      AND turn.owner_actor_id = @actor_id::uuid
      AND turn.kind = 'TURN' AND turn.state <> 'DELETED'
), attempts AS (
    SELECT turn.id AS turn_id, turn.organization_id, turn.project_id,
           turn.owner_actor_id, turn.version AS turn_version, turn.spec AS turn_spec,
           coalesce(attempt.attempt, (turn.spec ->> 'attempt')::integer) AS attempt_number,
           coalesce(attempt.runtime_revision_id,
                    (turn.spec ->> 'runtimeRevisionId')::uuid) AS runtime_revision_id,
           coalesce(attempt.runtime_revision_version, revision.version) AS runtime_revision_version,
           CASE
               WHEN attempt.attempt IS NULL
                    OR attempt.attempt = (turn.spec ->> 'attempt')::integer
                   THEN turn.state
               ELSE attempt.state
           END AS attempt_state,
           coalesce(attempt.started_at, turn.created_at) AS occurred_at,
           coalesce(attempt.finished_at, execution.updated_at, turn.updated_at) AS updated_at,
           execution.id AS execution_id,
           execution.version AS execution_version,
           execution.runtime_revision_version AS execution_revision_version
    FROM turns AS turn
    LEFT JOIN control_plane.turn_attempts AS attempt ON attempt.turn_id = turn.id
    JOIN control_plane.resources AS revision
      ON revision.organization_id = turn.organization_id
     AND revision.project_id = turn.project_id
     AND revision.owner_actor_id = turn.owner_actor_id
     AND revision.id = coalesce(attempt.runtime_revision_id,
                                (turn.spec ->> 'runtimeRevisionId')::uuid)
     AND revision.kind = 'RUNTIME_REVISION' AND revision.state <> 'DELETED'
    LEFT JOIN control_plane.runtime_executions AS execution
      ON execution.organization_id = turn.organization_id
     AND execution.project_id = turn.project_id
     AND execution.turn_id = turn.id
     AND execution.attempt = coalesce(attempt.attempt, (turn.spec ->> 'attempt')::integer)
     AND execution.runtime_revision_id = coalesce(attempt.runtime_revision_id,
                                                  (turn.spec ->> 'runtimeRevisionId')::uuid)
     AND execution.runtime_revision_version = coalesce(attempt.runtime_revision_version,
                                                       revision.version)
), graph_ids AS (
    SELECT id FROM processes
    UNION SELECT id FROM turns
    UNION SELECT nullif(turn.spec ->> 'sessionId', '')::uuid FROM turns AS turn
    UNION SELECT runtime_revision_id FROM attempts
    UNION SELECT execution_id FROM attempts WHERE execution_id IS NOT NULL
), artifacts AS (
    SELECT artifact.*
    FROM control_plane.resources AS artifact
    JOIN graph_ids ON graph_ids.id = artifact.parent_id
    WHERE artifact.organization_id = @organization_id::uuid
      AND artifact.project_id = @project_id::uuid
      AND artifact.owner_actor_id = @actor_id::uuid
      AND artifact.kind = 'ARTIFACT' AND artifact.state <> 'DELETED'
    UNION
    SELECT artifact.*
    FROM turns AS turn
    CROSS JOIN LATERAL (
        SELECT nullif(turn.spec ->> 'promptArtifactId', '')::uuid AS id
        UNION SELECT nullif(turn.spec ->> 'resultArtifactId', '')::uuid
        UNION SELECT nullif(input.value ->> 'artifactId', '')::uuid
              FROM jsonb_array_elements(coalesce(turn.spec -> 'inputArtifacts', '[]'::jsonb)) AS input(value)
    ) AS reference
    JOIN control_plane.resources AS artifact ON artifact.id = reference.id
    WHERE artifact.organization_id = @organization_id::uuid
      AND artifact.project_id = @project_id::uuid
      AND artifact.owner_actor_id = @actor_id::uuid
      AND artifact.kind = 'ARTIFACT' AND artifact.state <> 'DELETED'
)
SELECT 'PROCESS' AS node_type, process.id::text, process.state,
       coalesce(process.spec ->> 'parentProcessRunId', ''), process.id::text,
       '' AS session_id, '' AS turn_id, '' AS runtime_revision_id,
       '' AS predecessor_id, '' AS successor_id,
       process.version, 0::bigint AS runtime_revision_version, 0::integer AS attempt,
       process.created_at, process.updated_at
FROM processes AS process
UNION ALL
SELECT 'ATTEMPT', coalesce(attempt.execution_id,
           md5(attempt.turn_id::text || ':' || attempt.attempt_number::text)::uuid)::text,
       attempt.attempt_state, '', attempt.turn_spec ->> 'processRunId',
       attempt.turn_spec ->> 'sessionId', attempt.turn_id::text,
       attempt.runtime_revision_id::text,
       coalesce(attempt.turn_spec ->> 'predecessorTurnId', ''), '' AS successor_id,
       coalesce(attempt.execution_version, attempt.turn_version),
       coalesce(attempt.execution_revision_version, attempt.runtime_revision_version),
       attempt.attempt_number, attempt.occurred_at, attempt.updated_at
FROM attempts AS attempt
UNION ALL
SELECT 'ARTIFACT', artifact.id::text, artifact.state, '', '' AS process_run_id,
       '' AS session_id, '' AS turn_id, '' AS runtime_revision_id,
       '' AS predecessor_id, '' AS successor_id,
       artifact.version, 0::bigint, 0::integer, artifact.created_at, artifact.updated_at
FROM artifacts AS artifact
ORDER BY 14, 2;
