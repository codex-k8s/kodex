-- name: RunGraphNodes
WITH process_ids AS (
    SELECT *
    FROM control_plane.owner_run_graph_process_ids(
        @organization_id::uuid,
        @project_id::uuid,
        @actor_id::uuid,
        @process_run_id::uuid,
        @graph_process_limit
    )
), limited_processes AS (
    SELECT resource.*, process_ids.graph_overflow
    FROM process_ids
    JOIN control_plane.resources AS resource ON resource.id = process_ids.process_id
), turns AS (
    SELECT turn.*
    FROM control_plane.resources AS turn
    JOIN limited_processes AS process ON process.id = nullif(turn.spec ->> 'processRunId', '')::uuid
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
    ORDER BY turn.id, attempt_number, coalesce(execution.id, turn.id)
    LIMIT @graph_node_limit
), nodes AS (
SELECT 1 AS sort_kind, 'PROCESS' AS node_type, process.id::text AS node_id, process.state,
       coalesce(process.spec ->> 'parentProcessRunId', '') AS parent_process_run_id,
       process.id::text AS process_run_id,
       '' AS session_id, '' AS turn_id,
       coalesce(process.spec ->> 'currentRuntimeRevisionId', process.spec ->> 'runtimeRevisionId', '') AS runtime_revision_id,
       '' AS predecessor_id, '' AS successor_id,
       process.version,
       coalesce((process.spec ->> 'currentRuntimeRevisionVersion')::bigint, 0::bigint) AS runtime_revision_version,
       0::integer AS attempt,
       process.created_at, process.updated_at, process.name AS display_name
FROM limited_processes AS process
UNION ALL
SELECT 2, 'ATTEMPT', coalesce(attempt.execution_id,
           md5(attempt.turn_id::text || ':' || attempt.attempt_number::text)::uuid)::text,
       attempt.attempt_state, '', attempt.turn_spec ->> 'processRunId',
       attempt.turn_spec ->> 'sessionId', attempt.turn_id::text,
       attempt.runtime_revision_id::text,
       coalesce(attempt.turn_spec ->> 'predecessorTurnId', ''), '' AS successor_id,
       coalesce(attempt.execution_version, attempt.turn_version),
       coalesce(attempt.execution_revision_version, attempt.runtime_revision_version),
       attempt.attempt_number, attempt.occurred_at, attempt.updated_at,
       'Попытка ' || attempt.attempt_number::text
FROM attempts AS attempt
), bounded_nodes AS (
    SELECT * FROM nodes ORDER BY sort_kind, node_id::uuid LIMIT @graph_node_limit
), bounded AS (
    SELECT *, (SELECT bool_or(process.graph_overflow) FROM limited_processes AS process)
        OR (SELECT count(*) FROM nodes) > @graph_hard_limit AS graph_overflow
    FROM bounded_nodes
)
SELECT node_type, node_id, state, parent_process_run_id, process_run_id,
       session_id, turn_id, runtime_revision_id, predecessor_id, successor_id,
       version, runtime_revision_version, attempt, created_at, updated_at, display_name,
       graph_overflow
FROM bounded
WHERE (
    @after_node_type = ''
    OR (sort_kind, node_id::uuid) > (
        CASE @after_node_type WHEN 'PROCESS' THEN 1 WHEN 'ATTEMPT' THEN 2 ELSE 3 END,
        nullif(@after_node_id, '')::uuid
    )
)
ORDER BY sort_kind, node_id::uuid
LIMIT @limit;
